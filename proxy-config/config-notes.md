```jsonc
{
  "log": {
    "level": "",
    "timestamp": true         // 日志中显示时间戳
  },
  "inbounds": [
    {
      "type": "mixed",        // 同时支持 HTTP 和 SOCKS 代理入站
      "tag": "browser-in",    // 浏览器使用的入站代理标签
      "listen": "127.0.0.1",  // 只监听本机地址
      "listen_port": 7890     // 浏览器代理端口
    },
    {
      "type": "socks",        // 只接受 SOCKS 代理入站
      "tag": "game-in",       // 游戏使用的入站代理标签
      "listen": "127.0.0.1",  // 只监听本机地址
      "listen_port": 7891     // 游戏代理端口
    }
  ],
  "outbounds": [
    {
      "type": "socks",        // 通过 SOCKS 出站连接上游代理
      "tag": "my-socks5",     // 本地 SOCKS5 上游代理标签
      "server": "127.0.0.1",  // 上游 SOCKS5 代理地址
      "server_port": 1080     // 上游 SOCKS5 代理端口
    },
    {
      "type": "direct",       // 直连
      "tag": "direct"         // 直连出站标签
    }
  ],
  "route": {
    "rules": [
      {
        "inbound": [
          "browser-in"
        ],
        "action": "route",
        "outbound": "my-socks5" // 浏览器入站流量转发到本地 SOCKS5 上游代理
      },
      {
        "inbound": [
          "game-in"
        ],
        "action": "route",
        "outbound": "direct"   // 游戏流量直连
      }
    ],
    "final": "direct"          // 未命中上面规则的流量默认直连
  }
}
```
