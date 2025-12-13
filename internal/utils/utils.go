package utils

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/go-playground/errors/v5"
	"github.com/pelletier/go-toml/v2"
	"go.yaml.in/yaml/v4"
)

type config struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Value string `json:"value"`
}

func GetConfig[T any](url string) (T, error) {
	var res T

	httpResp, err := http.Get(url)
	if err != nil {
		return res, errors.Wrap(err, "failed to send request")
	}

	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return res, errors.Wrap(err, "failed to read response body")
	}

	var resp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    config `json:"data"`
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
