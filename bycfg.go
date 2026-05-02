package bycfg

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"sync"
	"time"

	"github.com/go-playground/errors/v5"
	"github.com/pelletier/go-toml/v2"
	"go.yaml.in/yaml/v4"
)

func getConfig[T any](url string, httpClient http.Client) (T, error) {
	var res T

	httpResp, err := httpClient.Get(url)
	if err != nil {
		return res, errors.Wrap(err, "failed to perform request")
	}

	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return res, errors.Wrap(err, "failed to read response body")
	}

	var resp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			Name  string `json:"name"`
			Type  string `json:"type"`
			Value string `json:"value"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return res, errors.Wrap(err, "failed to unmarshal response")
	}

	config := resp.Data

	switch any(res).(type) {
	case string:
		return any(config.Value).(T), nil
	default:
		switch config.Type {
		case "json":
			err := json.Unmarshal([]byte(config.Value), &res)
			if err != nil {
				return res, errors.Wrap(err, "failed to unmarshall json config")
			}
			return res, nil
		case "yaml":
			err := yaml.Unmarshal([]byte(config.Value), &res)
			if err != nil {
				return res, errors.Wrap(err, "failed to unmarshall yaml config")
			}
			return res, nil
		case "toml":
			err := toml.Unmarshal([]byte(config.Value), &res)
			if err != nil {
				return res, errors.Wrap(err, "failed to unmarshall toml config")
			}
			return res, nil
		default:
			return res, fmt.Errorf("unexpected kv config type %s", config.Type)
		}
	}
}

type BycfgLogger interface {
	Info(msg string, keyvals ...any)
	Error(msg string, keyvals ...any)
}

type BycfgParams[T any] struct {
	ConfigCenterHost string
	HttpClient       http.Client
	NeedRestart      func(oldValue, newValue T) bool
	ReloadCallback   func(newValue T) error
	Logger           BycfgLogger
}

type dummyLogger struct{}

func (dummyLogger) Info(string, ...any)  {}
func (dummyLogger) Error(string, ...any) {}

var defaultLogger BycfgLogger = dummyLogger{}

func (p *BycfgParams[T]) setDefault() {
	if p.ConfigCenterHost == "" {
		p.ConfigCenterHost = "config-center-next.config-center-next"
	}
	if p.NeedRestart == nil {
		p.NeedRestart = func(oldValue T, newValue T) bool {
			return reflect.DeepEqual(oldValue, newValue)
		}
	}
	if p.ReloadCallback == nil {
		p.ReloadCallback = func(newValue T) error {
			return nil
		}
	}
	if p.Logger == nil {
		p.Logger = defaultLogger
	}
}

type Bycfg[T any] struct {
	configCenterHost string
	applicationName  string
	configName       string
	needRestart      func(oldValue, newValue T) bool
	reloadCallback   func(newValue T) error
	httpClient       http.Client
	logger           BycfgLogger

	muConfig sync.RWMutex
	config   T

	muReload sync.Mutex
}

func (c *Bycfg[T]) getConfig() (T, error) {
	return getConfig[T](fmt.Sprintf("http://%s/client/%s/%s", c.configCenterHost, c.applicationName, c.configName), c.httpClient)
}

func (c *Bycfg[T]) restart() error {
	_, err := c.httpClient.Post(fmt.Sprintf("http://%s/client/%s/restart", c.configCenterHost, c.applicationName), "", nil)
	return errors.Wrap(err, "failed to restart pod")
}

func New[T any](applicationName string, configName string, p *BycfgParams[T]) (*Bycfg[T], error) {
	params := BycfgParams[T]{}
	if p != nil {
		params = *p
	}
	params.setDefault()

	res := Bycfg[T]{
		configCenterHost: params.ConfigCenterHost,
		applicationName:  applicationName,
		configName:       configName,
		needRestart:      params.NeedRestart,
		reloadCallback:   params.ReloadCallback,
		httpClient:       params.HttpClient,
		logger:           params.Logger,
	}

	value, err := res.getConfig()
	if err != nil {
		return nil, errors.Wrap(err, "failed to get config")
	}

	res.config = value
	return &res, nil
}

func (c *Bycfg[T]) Get() T {
	c.muConfig.RLock()
	defer c.muConfig.RUnlock()
	return c.config
}

func (c *Bycfg[T]) Reload() (err error) {
	c.muReload.Lock()
	defer c.muReload.Unlock()

	newValue, err := c.getConfig()
	if err != nil {
		return errors.Wrap(err, "failed to get config")
	}

	c.muConfig.RLock()
	oldValue := c.config
	c.muConfig.Unlock()

	if c.needRestart(oldValue, newValue) {
		return errors.Wrap(c.restart(), "failed to restart")
	}

	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("failed to call reload callback: %v", r)
		}
	}()
	err = c.reloadCallback(newValue)
	if err != nil {
		return errors.Wrap(err, "failed to call reload callback")
	}

	c.muConfig.Lock()
	c.config = newValue
	c.muConfig.Unlock()

	return nil
}

func (c *Bycfg[T]) Watch(d time.Duration, ctx context.Context) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(d):
				c.logger.Info("starting scheduled config reload", "config_name", c.configName)
				err := c.Reload()
				if err != nil {
					c.logger.Error("failed to reload config", "config_name", c.configName, "error", err)
				} else {
					c.logger.Info("successfully reloaded config", "config_name", c.configName)
				}
			}
		}
	}()
}
