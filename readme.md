# go-wisp

go-wisp is a lightweight proxy server written in Go that multiplexes multiple TCP/UDP sockets over a single websocket connection. It focuses on low overhead and robust error handling, making it an efficient proxy solution for various networking needs.

## Features

- **Multiplexing:** Support for handling multiple TCP/UDP streams over a single connection.
- **Efficient Data Handling:** Built-in buffering and flow control to manage data transmission effectively.
- **Websocket Integration:** Standard websocket connections for easy client-server communication.

## Installation

1. **Clone the Repository:**

    ```sh
    git clone https://github.com/TheFalloutOf76/go-wisp
    cd go-wisp
    ```

2. **Install Dependencies:**

    ```sh
    go get .
    ```

3. **Build the Project:**

    ```sh
    go build
    ```

## Usage
1. Download a pre-compiled binary that matches your target platform from [builds](https://github.com/TheFalloutOf76/go-wisp/releases/tag/builds).
2. Unzip all the files.
3. Configure the server by editing the `config.json` file in the project root.
4. Run `go-wisp`.
5. Connect your clients to the server using a websocket connection. The server takes care of multiplexing and routing to the appropriate TCP/UDP streams.