Mini BitTorrent-style P2P file sharing system written in Go.
Tracker-based peer discovery + gRPC peer-to-peer chunk transfer. Designed for learning distributed systems, Go concurrency.

What it is:

- A lightweight P2P system where:

- Files are split into chunks and hashed (SHA-256).

- A tracker (HTTP) tracks which peers host which files.

- Peers run a gRPC server (serve chunks) and a client (download chunks from other peers).

- Downloads fetch chunks in parallel from multiple peers, verify hashes, and assemble the file.

- Simple, testable, and runnable locally (Docker Compose or native).
