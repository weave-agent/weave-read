# weave-read

Read tool extension for [weave](https://github.com/weave-agent/weave) — an event-driven coding agent framework.

## Fork & Customize

1. Fork this repo
2. Edit the extension implementation
3. Install your fork: `weave install github.com/<you>/weave-read --name read`

The `--name read` ensures your fork shadows the official extension.

## Install

```bash
weave install github.com/weave-agent/weave-read --name read
```

## Guardian Policy

The `read` tool sends each resolved read path to the registered Guardian before sandbox or file read operations. It uses `sdk.GuardianActionRead`.

If no Guardian is registered, reads proceed normally. Guardian allow decisions continue to sandbox checks and the normal read path. Guardian block decisions return a tool error beginning with `guardian: blocked`; Guardian errors return `guardian: <error>`.

## Development

```bash
git clone git@github.com:weave-agent/weave-read.git
cd weave-read

# Add temporary replace for local SDK (don't commit this)
echo 'replace github.com/weave-agent/weave => /path/to/local/weave' >> go.mod

go test ./...
```

## License

Same as the main weave project.
