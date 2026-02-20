# Config & Hot Reload

## Dynamic config keys
- remote.enabled
- remote.relay_url
- model.default
- compaction.algorithm
- tools.read.enabled
- tools.write.enabled

## Hot apply
- ConfigStore publishes changes to subscribers.
- RemoteClient starts/stops immediately.
- Model changes apply to new runs only.
