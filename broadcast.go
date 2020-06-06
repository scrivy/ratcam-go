package main

import (
	"context"
	"encoding/gob"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
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

func broadcast() {
	newConnChan = make(chan client, 10)
	askForStreaming = make(chan struct{}, 10)
	streaming = make(chan bool, 10)
	go dialAndReceiveFrames()

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		htmlIndex, err := ioutil.ReadFile("/usr/lib/ratcam/index.html")
		if err != nil {
			log.Println(err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Write(htmlIndex)
	})
	mux.HandleFunc("/ws", wsHandler)

	log.Println("Listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
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
					time.Sleep(time.Second)
					continue
				}

				conn, err = net.DialTimeout("tcp", config.CameraAddr, 5*time.Second)
				if err != nil {
					log.Printf("%+v\n", errors.WithStack(err))
					continue
				}
				decoder = gob.NewDecoder(conn)
				connected = true
			} else if connected && len(clients) == 0 {
				conn.Close()
				connected = false
				log.Println("disconnecting due to lack of clients")
				continue
			}

			frame := make([]byte, 256000)
			err = decoder.Decode(&frame)
			if err != nil {
				log.Printf("%+v\n", errors.WithStack(err))
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
		log.Printf("%+v\n", errors.WithStack(err))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// get read ip if using nginx
	realIP := r.Header.Get("X-Real-Ip")
	if realIP == "" {
		realIP = r.RemoteAddr
	}
	log.Println("accepted connection from", realIP)

	// forward to local address if not currently streaming
	askForStreaming <- empty
	isStreaming := <-streaming
	if !isStreaming && config.HomeIp != "" && strings.HasPrefix(r.Header.Get("X-Real-Ip"), config.HomeIp) {
		log.Println("redirecting to local network")
		wsutil.WriteServerText(conn, []byte(config.LocalAddr))
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
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
				log.Println("read websocket go routine closed")
			}
		}()
		for {
			select {
			case <-ctx.Done():
				if config.Debug {
					log.Println("read websocket go routine closed from ctx.Done()")
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
						log.Printf("%+v\n", errors.WithStack(err))
					}
					return
				}

				if frame.Header.OpCode == ws.OpClose {
					if config.Debug {
						statusCode, reason := ws.ParseCloseFrameDataUnsafe(frame.Payload)
						log.Printf("read websocket: received ws.OpClose: statusCode: %d, reason: %s\n", statusCode, reason)
					}
					return
				}
				log.Printf("read websocket: payload %s\n", frame.Payload)
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
				log.Println("write websocket: go routine closed")
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
						log.Printf("%+v\n", errors.WithStack(err))
					}
					return
				}
			}
		}
	}()
}
