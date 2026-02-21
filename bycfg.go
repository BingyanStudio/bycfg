package bycfg

import (
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/BingyanStudio/bycfg/internal/set"
	"github.com/BingyanStudio/bycfg/internal/utils"
	"github.com/go-playground/errors/v5"
)

var muCallbackRegistry sync.RWMutex
var callbackRegistry map[string]func() error

func RegisterCallback(name string, callback func() error) {
	muCallbackRegistry.Lock()
	defer muCallbackRegistry.Unlock()
	callbackRegistry[name] = callback
}

func getCallback(name string) (func() error, bool) {
	muCallbackRegistry.RLock()
	defer muCallbackRegistry.RUnlock()
	callback, exists := callbackRegistry[name]
	return callback, exists
}

type Bycfg[T any] struct {
	url           string
	defaultReload func() error
	onReloadError func(error)

	muConfig sync.RWMutex
	config   T
}

// assume newValue.Type() == oldValue.Type()
func collectCallbacks(newValue, oldValue reflect.Value, callback *string) set.Set[string] {
	if newValue.Kind() == reflect.Pointer {
		if newValue.IsNil() != oldValue.IsNil() {
			return set.FromPtr(callback)
		}

		if newValue.IsNil() {
			return set.New[string]()
		}

		newValue, oldValue = newValue.Elem(), oldValue.Elem()
	}

	if newValue.Kind() != reflect.Struct {
		if reflect.DeepEqual(newValue.Interface(), oldValue.Interface()) {
			return set.New[string]()
		} else {
			return set.FromPtr(callback)
		}
	}

	callbacks := set.New[string]()

	for i := range newValue.NumField() {
		newValueField, oldValueField := newValue.Field(i), oldValue.Field(i)

		var fieldCallback *string
		val, exist := newValue.Type().Field(i).Tag.Lookup("bycfg")
		if exist {
			fieldCallback = &val
		}

		fieldCallbacks := collectCallbacks(newValueField, oldValueField, fieldCallback)

		if fieldCallbacks != nil {
			callbacks.Insert(fieldCallbacks)
		} else {
			return set.FromPtr(callback)
		}
	}

	return callbacks
}

func New[T any](url string,
	defaultReload func() error,
	onReloadError func(error),
) (Bycfg[T], error) {
	c, err := utils.GetConfig[T](url)
	if err != nil {
		return Bycfg[T]{}, errors.Wrap(err, "failed to initalize config")
	}

	if defaultReload == nil {
		defaultReload = func() error { return nil }
	}
	if onReloadError == nil {
		onReloadError = func(err error) { fmt.Printf("%+v\n", err) }
	}

	return Bycfg[T]{
		url:           url,
		defaultReload: defaultReload,
		onReloadError: onReloadError,
		config:        c,
	}, nil
}

func (b *Bycfg[T]) GetConfig() T {
	b.muConfig.RLock()
	defer b.muConfig.RUnlock()
	return b.config
}

func (b *Bycfg[T]) ReloadConfig() error {
	b.muConfig.Lock()
	defer b.muConfig.Unlock()

	oldConfig := b.config
	newConfig, err := utils.GetConfig[T](b.url)
	if err != nil {
		return errors.Wrap(err, "failed to get new config")
	}

	callbacks := collectCallbacks(reflect.ValueOf(newConfig), reflect.ValueOf(oldConfig), nil)

	b.config = newConfig
	if callbacks == nil {
		err = b.defaultReload()
	} else {
		for callbackName := range callbacks {
			if callbackName == "" {
				continue
			}

			callback, exists := getCallback(callbackName)
			if !exists {
				err = fmt.Errorf("callback %s is not registered", callbackName)
				break
			}

			err = callback()
			if err != nil {
				break
			}
		}
	}
	if err != nil {
		b.config = oldConfig
		return errors.Wrap(err, "failed to reload config")
	}

	return nil
}

func (b *Bycfg[T]) WatchConfig(d time.Duration) {
	go func() {
		ticker := time.NewTicker(d)
		defer ticker.Stop()
		for range ticker.C {
			err := b.ReloadConfig()
			if err != nil {
				b.onReloadError(err)
			}
		}
	}()
}

func init() {
	callbackRegistry = map[string]func() error{}
}
