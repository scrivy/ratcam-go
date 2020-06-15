package main

import (
	"context"
	"encoding/gob"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
	"github.com/pkg/errors"
)

type client struct {
	conn    net.Conn
	picChan chan []byte
	ctx     context.Context
}

var (
	newConnChan     chan client
	askForStreaming chan struct{}
	streaming       chan bool

	empty struct{}
)

func broadcast(indexHtmlPath string) {
	newConnChan = make(chan client, 10)
	askForStreaming = make(chan struct{}, 10)
	streaming = make(chan bool, 10)
	go dialAndReceiveFrames()

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		htmlIndex, err := ioutil.ReadFile(indexHtmlPath)
		if err != nil {
			fmt.Println(err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Write(htmlIndex)
	})
	mux.HandleFunc("/ws", wsHandler)

	fmt.Println("Listening on :8080")
	err := http.ListenAndServe(":8080", mux)
	if err != nil {
		fmt.Printf("%+v\n", errors.WithStack(err))
		os.Exit(1)
	}
}

func dialAndReceiveFrames() {
	clients := map[net.Conn]client{}
	var connected bool
	var conn net.Conn
	var err error
	var decoder *gob.Decoder

	for {
		select {
		case <-askForStreaming:
			streaming <- connected
		case c := <-newConnChan:
			clients[c.conn] = c
		default:
			// dial if needed
			if !connected {
				if len(clients) == 0 {
					time.Sleep(100 * time.Millisecond)
					continue
				}

				conn, err = net.DialTimeout("tcp", config.CameraIP+":"+config.CameraPort, 5*time.Second)
				if err != nil {
					fmt.Printf("%+v\n", errors.WithStack(err))
					continue
				}
				decoder = gob.NewDecoder(conn)
				connected = true
			} else if connected && len(clients) == 0 {
				conn.Close()
				connected = false
				fmt.Println("disconnecting due to lack of clients")
				continue
			}

			frame := make([]byte, 256000)
			err = decoder.Decode(&frame)
			if err != nil {
				fmt.Printf("%+v\n", errors.WithStack(err))
				conn.Close()
				connected = false
				continue
			}

			for _, c := range clients {
				if c.ctx.Err() == nil {
					if len(c.picChan) < cap(c.picChan) {
						c.picChan <- frame
					}
				} else {
					delete(clients, c.conn)
				}
			}
		}
	}
}

func wsHandler(w http.ResponseWriter, r *http.Request) {
	conn, _, _, err := ws.UpgradeHTTP(r, w)
	if err != nil {
		fmt.Printf("%+v\n", errors.WithStack(err))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// get read ip if using nginx
	realIP := r.Header.Get("X-Real-Ip")
	if realIP == "" {
		realIP = r.RemoteAddr
	}
	fmt.Println("accepted connection from", realIP)

	// forward to local address if not currently streaming
	if config.RedirectToLocal {
		askForStreaming <- empty
		isStreaming := <-streaming
		if !isStreaming && config.HomeIPv6 != "" {
			if strings.HasPrefix(r.Header.Get("X-Real-Ip"), config.HomeIPv6) || (realIP == config.CameraIP && config.CameraIP != "127.0.0.1") {
				fmt.Println("redirecting to local network")
				wsutil.WriteServerText(conn, []byte(config.LocalAddr))
				return
			}
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(config.MaxStreamDurationMinutes)*time.Minute)
	c := client{
		conn:    conn,
		picChan: make(chan []byte, 1),
		ctx:     ctx,
	}
	newConnChan <- c

	// read websocket
	go func() {
		defer func() {
			cancel()
			if config.Debug {
				fmt.Println("read websocket go routine closed")
			}
		}()
		for {
			select {
			case <-ctx.Done():
				if config.Debug {
					fmt.Println("read websocket go routine closed from ctx.Done()")
				}
				return
			default:
				frame, err := ws.ReadFrame(conn)
				if err != nil {
					switch err {
					case io.EOF:
						return
					}
					switch err.Error() {
					case "EOF":
						return
					}
					switch err.(type) {
					case *net.OpError:
					default:
						fmt.Printf("%+v\n", errors.WithStack(err))
					}
					return
				}

				if frame.Header.OpCode == ws.OpClose {
					if config.Debug {
						statusCode, reason := ws.ParseCloseFrameDataUnsafe(frame.Payload)
						fmt.Printf("read websocket: received ws.OpClose: statusCode: %d, reason: %s\n", statusCode, reason)
					}
					return
				}
				fmt.Printf("read websocket: payload %s\n", frame.Payload)
			}
		}
	}()

	// write websocket
	go func() {
		defer func() {
			cancel()
			c.conn.Close()
			close(c.picChan)
			if config.Debug {
				fmt.Println("write websocket: go routine closed")
			}
		}()
		for {
			select {
			case <-ctx.Done():
				return
			case pic := <-c.picChan:
				if len(c.picChan) != 0 { // dropping frame to get more recent pic
					continue
				}
				err := wsutil.WriteServerBinary(conn, pic)
				if err != nil {
					switch err.(type) {
					case *net.OpError:
					default:
						fmt.Printf("%+v\n", errors.WithStack(err))
					}
					return
				}
			}
		}
	}()
}
