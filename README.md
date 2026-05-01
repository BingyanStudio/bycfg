# bycfg

Go SDK for [config-center-next](https://github.com/BingyanStudio/config-center-next)。

## 快速开始

```go
var (
	globalComment   string
	muGlobalComment sync.RWMutex
)

type Config struct {
	DSN     string `json:"dsn"`
	Comment string `json:"comment"`
}

func main() {
	// 使用默认配置
	cfg, _ := bycfg.New[Config](os.Getenv("APP_NAME"), "config.json", nil)

	// 使用自定义配置
	cfg_custom, _ := bycfg.New(os.Getenv("APP_NAME"), "config.json", &bycfg.BycfgParams[Config]{
		// 使用其他的配置中心实例，现在应该用不到...
		ConfigCenterHost: "another.config-center-next.instance",

		// 为用于获取配置的 http client 设置参数
		HttpClient: http.Client{Timeout: time.Second * 5},

		// 此回调在获取新配置值后被调用，通过返回值指示 Bycfg 是否向配置中心发起重启请求
		NeedRestart: func(oldValue, newValue Config) bool {
			return oldValue.DSN != newValue.DSN
		},

		// 此回调在 NeedRestart 传回 false 时被调用，处理无需重启就能处理的配置项变动
		ReloadCallback: func(newValue Config) error {
			muGlobalComment.Lock()
			globalComment = newValue.Comment
			muGlobalComment.Unlock()
			return nil
		},

		// 注入 Logger
		Logger: slog.Default(),
	})

	// 获取配置
	fmt.Printf("%+v\n", cfg.Get())

	// 手动重载
	cfg.Reload()


	// 启动热更新
	ctx, cancel := context.WithCancel(context.Background())
	cfg.Watch(time.Minute, ctx)

	// 停止热更新
	cancel()
}
```

## 回调

如果未设置 `NeedRestart`，默认回调的行为是通过 `reflect.DeepEqual` 比对旧配置值和新配置值，若不匹配返回 `true`．

如果未设置 `ReloadCallback`，默认回调的行为是什么都不做．

`Reload` 过程中发生错误时，SDK 将会通过注入的 `Logger` 记录，并不会更新配置值．

`ReloadCallback` 若 panic，将会 recover．

若确实需要使用此功能，请注意新旧配置值都已通过函数参数传入，不要在回调中利用 `Bycfg.Get` 访问配置，不然会导致死锁．
