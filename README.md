# bycfg

## 快速开始

```go
package readme

import (
    "fmt"
    "time"

    "github.com/BingyanStudio/bycfg"
)

// 定义配置结构体，在 struct tag 中指定对应字段变化时，需要调用的回调函数的 id
type Config struct {
    Server struct {
        Port int `json:"port"`
        Host string `json:"host"`
    } `json:"server" bycfg:"restart_server"`
    Database struct {
        DSN string `json:"dsn"`
    } `json:"database" bycfg:"reconnect_db"`
}

func main() {
    // 注册回调函数
    bycfg.RegisterCallback("restart_server", func() error {
        fmt.Println("Restarting server...")
        return nil
    })
    bycfg.RegisterCallback("reconnect_db", func() error {
        fmt.Println("Reconnecting to database...")
        return nil
    })

    // 创建配置实例
    cfg, err := bycfg.New[Config](
        // config center 配置链接（非 raw）
        "http://cc-server.config-center/example/config.json",

        // 配置默认重载回调
        func() error {
            fmt.Println("Default reload triggered")
            return nil
        },

        // 配置重载错误处理
        func(err error) {
            fmt.Printf("Reload error: %v\n", err)
        },
    )
    if err != nil {
        panic(err)
    }

    // 启动热更新
    cfg.WatchConfig(30 * time.Second)

    // 访问配置
    fmt.Printf("%+v", cfg.GetConfig());

    select {}
}
```

## 回调机制

### 回调执行逻辑

1. 配置变更时，SDK 会比较新旧配置值
2. 如果结构体字段发生变化，且该字段有 `bycfg` tag，则执行对应回调
3. 如果字段无 tag 但其子字段有回调，递归收集
4. 如果没有任何字段指定回调，则执行 `defaultReload`
