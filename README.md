# qBit-Ntfy Sidecar

A lightweight Go sidecar for Kubernetes to monitor qBittorrent downloads and send real-time progress updates to [ntfy.sh](https://ntfy.sh).

## Features
- **Event-Driven**: Only runs when triggered by qBittorrent (zero idle CPU usage).
- **Real-time Progress**: Sends updates with ASCII progress bars, speed, and ETA.
- **Completion Alerts**: High-priority notification when download finishes.
- **Secure**: Runs purely on localhost within the Pod.

## Installation

### 1. Deploy Sidecar
Add the sidecar container to your qBittorrent deployment. See `deployment-example.yaml` for a full example.

```yaml
- name: ntfy-sidecar
  image: ghcr.io/vehkiya/qbit-ntfy-sidecar:latest
  env:
    - name: QBIT_USER
      value: "admin"
    - name: QBIT_PASS
      value: "secret"
    - name: NTFY_TOPIC
      value: "https://ntfy.sh/uptime_topic"
```

### 2. Configure qBittorrent
1. Open qBittorrent Web UI (`Tools > Options > Downloads`).
2. Check **"Run external program on torrent added"**.
3. Enter the trigger command:
   ```bash
   curl -X POST "http://localhost:9090/track?hash=%I"
   ```

## Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `QBIT_HOST` | qBittorrent API URL | `http://localhost:8080` |
| `QBIT_USER` | Web UI Username | **Required** |
| `QBIT_PASS` | Web UI Password | **Required** |
| `NTFY_TOPIC` | Ntfy Topic URL | **Required** |

## Building Locally
```bash
go build -o sidecar main.go
```

## Docker Build
```bash
docker build -t qbit-ntfy-sidecar .
```
