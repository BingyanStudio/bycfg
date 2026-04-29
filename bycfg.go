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

type Bycfg[T any] struct {
	configCenterHost string
	applicationName  string
	configName       string
	reloadCallback   func(T, T) (bool, error)
	httpClient       http.Client

	muConfig sync.RWMutex
	config   T
}

func (c *Bycfg[T]) getConfig() (T, error) {
	return getConfig[T](fmt.Sprintf("http://%s/client/%s/%s", c.configCenterHost, c.applicationName, c.configName), c.httpClient)
}

func (c *Bycfg[T]) restart() error {
	_, err := c.httpClient.Post(fmt.Sprintf("http://%s/client/%s/restart", c.configCenterHost, c.applicationName), "", nil)
	return errors.Wrap(err, "failed to restart pod")
}

type NewBycfgParams[T any] struct {
	configCenterHost string
	httpClient       http.Client
	reloadCallback   func(T, T) (bool, error)
}

func (p *NewBycfgParams[T]) setDefault() {
	if p.configCenterHost == "" {
		p.configCenterHost = "config-center-next.config-center-next"
	}
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

func (c *Bycfg[T]) Reload() error {
	newValue, err := c.getConfig()
	if err != nil {
		return errors.Wrap(err, "failed to get config")
	}

	c.muConfig.Lock()
	oldValue := c.config
	c.config = newValue
	c.muConfig.Unlock()

	defer func() {
		if r := recover(); r != nil {
			// TODO recover when user callback failed
		}
	}()

	needRestart, err := c.reloadCallback(oldValue, newValue)
	if err != nil {
		return errors.Wrap(err, "failed to execute user reload callback")
	}
	if needRestart {
		err := c.restart()
		if err != nil {
			return errors.Wrap(err, "failed to restart")
		}
	}

	return nil
}

func (c *Bycfg[T]) Watch(d time.Duration, ctx context.Context) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(d):
				err := c.Reload()
				if err != nil {
					// log error
				}
			}
		}
	}()
}
