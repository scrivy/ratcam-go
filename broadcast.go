package main

import (
	"context"
	"encoding/gob"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/getsentry/raven-go"
	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
	"github.com/pkg/errors"
)

type client struct {
	conn    net.Conn
	picChan chan []byte
	ctx     context.Context
}

var newConnChan chan client

func broadcast() {
	newConnChan = make(chan client, 10)
	go dialAndReceiveFrames()

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		htmlIndex, err := ioutil.ReadFile("index.html")
		if err != nil {
			log.Println(err)
			raven.CaptureError(err, nil)
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
		case c := <-newConnChan:
			clients[c.conn] = c
		default:
			// dial if needed
			if !connected {
				if len(clients) == 0 { // block until new websocket
					log.Println("waiting for new connection")
					c := <-newConnChan
					clients[c.conn] = c
				}

				conn, err = net.DialTimeout("tcp", config.CameraAddr, 5*time.Second)
				if err != nil {
					fmt.Printf("%+v\n", errors.WithStack(err))
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

			frame := make([]byte, 100000)
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
		log.Println(err)
		raven.CaptureError(err, nil)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	c := client{
		conn:    conn,
		picChan: make(chan []byte, 2),
		ctx:     ctx,
	}
	newConnChan <- c

	// read websocket
	go func() {
		defer func() {
			cancel()
			log.Println("read websocket go routine closed")
		}()
		for {
			select {
			case <-ctx.Done():
				log.Println("read websocket go routine closed from ctx.Done()")
				return
			default:
				frame, err := ws.ReadFrame(conn)
				if err != nil {
					log.Println("read websocket: ws.ReadFrame: ", err)
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
						raven.CaptureError(err, nil)
					}
					return
				}

				if frame.Header.OpCode == ws.OpClose {
					statusCode, reason := ws.ParseCloseFrameDataUnsafe(frame.Payload)
					log.Printf("read websocket: received ws.OpClose: statusCode: %d, reason: %s\n", statusCode, reason)
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
			log.Println("write websocket: go routine closed")
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
					log.Println("write websocket: ", err)
					switch err.(type) {
					case *net.OpError:
					default:
						raven.CaptureError(err, nil)
					}
					return
				}
			}
		}
	}()
}
