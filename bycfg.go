package bycfg

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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

type Logger interface {
	Info(msg string, keyvals ...any)
	Error(msg string, keyvals ...any)
}

type NewBycfgParams[T any] struct {
	configCenterHost string
	httpClient       http.Client
	reloadCallback   func(oldValue T, newValue T) (needRestart bool, err error)
	logger           Logger
}

type dummyLogger struct{}

func (dummyLogger) Info(string, ...any)  {}
func (dummyLogger) Error(string, ...any) {}

var defaultLogger Logger = dummyLogger{}

func (p *NewBycfgParams[T]) setDefault() {
	if p.configCenterHost == "" {
		p.configCenterHost = "config-center-next.config-center-next"
	}
	if p.logger == nil {
		p.logger = defaultLogger
	}
}

type Bycfg[T any] struct {
	configCenterHost string
	applicationName  string
	configName       string
	reloadCallback   func(oldValue T, newValue T) (needRestart bool, err error)
	httpClient       http.Client
	logger           Logger

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

func New[T any](applicationName string, configName string, p *NewBycfgParams[T]) (*Bycfg[T], error) {
	params := NewBycfgParams[T]{}
	if p != nil {
		params = *p
	}
	params.setDefault()

	res := Bycfg[T]{
		configCenterHost: params.configCenterHost,
		applicationName:  applicationName,
		configName:       configName,
		reloadCallback:   params.reloadCallback,
		httpClient:       params.httpClient,
		logger:           params.logger,
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

	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("failed to call reload callback: %v", r)
		}
	}()
	needRestart, err := c.reloadCallback(oldValue, newValue)
	if err != nil {
		return errors.Wrap(err, "failed to call reload callback")
	}
	if needRestart {
		err := c.restart()
		if err != nil {
			return errors.Wrap(err, "failed to restart")
		}
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
				c.logger.Info("scheduled config reloading start", "config_name", c.configName)
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
