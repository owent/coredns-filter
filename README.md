# coredns-filter

filter plugin of coredns

## Name

*filter* - Filter DNS response message.

## Compilation

```txt
filter:github.com/owent/coredns-filter
```

This plugin should be add right after [cache][1].

```bash
sed -i.bak -r '/filter:.*/d' plugin.cfg
sed -i.bak '/cache:.*/i filter:github.com/owent/coredns-filter' plugin.cfg
go get github.com/owent/coredns-filter

go generate
```

## Syntax

```corefile
filter [command options...] {
  [command options...]
}
```

Available options:

+ `prefer <none/ipv4/ipv6>` : Return all/A/AAAA records when get both A and AAAA record in a answer.
+ `bogus-nxdomain [ip address/ip prefix...]` : Remove ip and set response code to NXDOMAIN .

## Examples

Enable filter:

```corefile
example.org {
    whoami
    forward . 8.8.8.8
    filter prefer ipv4 {
      bogus-nxdomain 127.0.0.1/30 123.125.81.12
    }
}
```

## See Also

## For Developers

### Debug Build

```bash
git clone --depth 1 https://github.com/coredns/coredns.git coredns
cd coredns
git reset --hard
sed -i.bak -r '/filter:.*/d' plugin.cfg
sed -i.bak '/cache:.*/a filter:github.com/owent/coredns-filter' plugin.cfg
go get -u github.com/owent/coredns-filter@main
# go get github.com/owent/coredns-filter@latest
go generate

env CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -gcflags=all="-N -l" -o build/linux/amd64/coredns
```

### Configure File For Debug

```conf
(default_dns_ip) {
  debug
  # errors
  forward . 119.29.29.29 223.5.5.5 1.0.0.1 94.140.14.140 2402:4e00:: 2400:3200::1 2400:3200:baba::1 2606:4700:4700::1001 2a10:50c0::1:ff {
    policy sequential
  }
  loop
  log
}

. {
  import default_dns_ip

  filter prefer ipv4
  filter {
    bogus-nxdomain 127.0.0.1/30 123.125.81.12
  }
}
```

### VSCode lanch example

```json
{
  "version": "0.2.0",
  "configurations": [
    {
      "name": "Launch Package",
      "type": "go",
      "request": "launch",
      "mode": "debug",
      "program": "${workspaceFolder}",
      "args": [
        "-dns.port=6813",
        "-conf=${workspaceFolder}/.vscode/test-coredns.conf",
        "-alsologtostderr"
      ],
      "showLog": true
    },
    {
      "name": "Launch Executable",
      "type": "go",
      "request": "launch",
      "mode": "exec",
      "program": "${workspaceFolder}/build/linux/amd64/coredns",
      "args": [
        "-dns.port=6813",
        "-conf=${workspaceFolder}/.vscode/test-coredns.conf",
        "-alsologtostderr"
      ],
      "cwd": "${workspaceFolder}/build",
      "showLog": true
    }
  ]
}
```

### Run

```bash
go get -v github.com/go-delve/delve/cmd/dlv

sudo build/linux/amd64/coredns -dns.port=6813 -conf test-coredns.conf

dig owent.net @127.0.0.1 -p 6813
```

[1]: https://coredns.io/plugins/cache/
