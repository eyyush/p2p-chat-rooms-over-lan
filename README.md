# chat-rooms
go-libp2p chat app

usage: 

```
go build -o chat
./chat [port] [optional room name]
```

choose any available port to listen on. join an optional room; you will only send and receive messages to others in the same room.

if no room name is specified, it will join `main`.


# Chat-Rooms:  How It Works

## 1. Initialization & Network Setup

```go
// Generate RSA keypair for the peer
priv, _, err := crypto.GenerateKeyPairWithReader(crypto.RSA, 2048, r)

// Create a libp2p host with custom options
host, err := libp2p.New(ctx, buildOpts(listenPort, priv)...)
```

- Each peer generates a random RSA keypair for identity
- A libp2p host is created to listen on a specific port
- The host is configured via `buildOpts()` to:
  - Listen on `0.0.0.0:port`
  - Use the generated private key for identity
  - Enable relay capability for peer communication

---

## 2. Peer Discovery via mDNS

```go
// Start mDNS discovery service
pchan := initMDNS(ctx, host, "noot")

// Wait for a peer to be discovered
peer := <-pchan
```

The app uses **mDNS (multicast DNS)** for local network discovery: 

- The `initMDNS()` function creates an mDNS service
- Peers automatically discover each other on the same local network
- When a peer is found, it's returned via a channel

---

## 3. Stream Handling

```go
// Define a protocol handler
pid := protocol.ID(fmt. Sprintf("/chat/1.1.0/%s", topic))
host.SetStreamHandler(pid, handleStream)

// Open stream connection to discovered peer
stream, err := host.NewStream(ctx, peer.ID, pid)
```

- Each chat room is identified by a **protocol ID** (e.g., `/chat/1.1.0/main`)
- When peers connect, they establish **bidirectional streams**
- `handleStream()` processes incoming connections

---

## 4. Message Exchange

```go
func readData(rw *bufio.ReadWriter) {
    for {
        str, err := rw.ReadString('\n')
        // Print received messages in green
        fmt. Printf("\x1b[32m%s\x1b[0m> ", str)
    }
}

func writeData(rw *bufio.ReadWriter) {
    for {
        sendData, err := stdReader.ReadString('\n')
        // Send user input over the stream
        rw.WriteString(sendData)
        rw.Flush()
    }
}
```

- `readData()` - Continuously reads incoming messages from the stream
- `writeData()` - Continuously reads from stdin and sends to the peer
- Both run concurrently via **goroutines**
- Messages are displayed in green (`\x1b[32m`)

---

## Usage

```bash
go build -o chat
./chat 5000 main          # Listen on port 5000, join 'main' room
./chat 5001 main          # Another instance on port 5001, same room
```

**How it works:**

1. First peer starts and waits for discovery
2. Second peer starts and discovers the first via mDNS
3. They automatically connect and form a bidirectional stream
4. Users can type messages which are sent in real-time
5. Default room is "main" if not specified

---

## Key Features

| Feature | Implementation |
|---------|---|
| **P2P Networking** | Uses go-libp2p for decentralized connections |
| **Peer Discovery** | mDNS for automatic local network discovery |
| **Chat Rooms** | Protocol IDs (`/chat/1.1.0/{roomname}`) separate conversations |
| **Async I/O** | Goroutines handle reading/writing independently |
| **Security** | RSA 2048-bit encryption for peer identity |

---

## Architecture Diagram

```
┌─────────────────────────────────────┐
│         Peer A (Port 5000)          │
│  ┌──────────────────────────────┐   │
│  │ libp2p Host + mDNS Discovery │   │
│  └──────────┬───────────────────┘   │
│             │ RSA Key + Identity    │
└─────────────┼─────────────────────┘
              │
        (mDNS Discovery)
              │
┌─────────────▼─────────────────────┐
│         Peer B (Port 5001)         │
│  ┌──────────────────────────────┐  │
│  │ libp2p Host + mDNS Discovery │  │
│  └──────────┬──────────────────┘   │
│             │ RSA Key + Identity   │
└─────────────┼──────────────────┘
              │
       (Bidirectional Stream)
              │
    ┌─────────▼──────────┐
    │  Protocol:   /chat/  │
    │  readData/writeData │
    │  (Chat Messages)    │
    └────────────────────┘
