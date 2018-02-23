package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	_ "net/http/pprof"
	"time"

	"github.com/blackjack/webcam"
	"github.com/getsentry/raven-go"
	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
	"gopkg.in/yaml.v2"
)

const webcamDevicePath = "/dev/video0"

var (
	camera      *webcam.Webcam
	config      Config
	newConnChan chan client
)

type Config struct {
	PixelFormat int
	Width       int
	Height      int
}

type client struct {
	conn    net.Conn
	picChan chan *[]byte
	ctx     context.Context
}

func main() {
	go func() {
		log.Fatal(http.ListenAndServe("localhost:6060", nil))
	}()

	// read and parse config
	rawConfig, err := ioutil.ReadFile("config.yaml")
	if err != nil {
		panic(err)
	}
	err = yaml.Unmarshal(rawConfig, &config)
	if err != nil {
		panic(err)
	}
	log.Printf("config:\n%#v\n", config)

	err = openCamera()
	if err != nil {
		panic(err)
	}
	defer camera.Close()
	dumpWebcamFormats()

	newConnChan = make(chan client, 10)
	go takePictures()

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

func wsHandler(w http.ResponseWriter, r *http.Request) {
	conn, _, _, err := ws.UpgradeHTTP(r, w, nil)
	if err != nil {
		log.Println(err)
		raven.CaptureError(err, nil)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	c := client{
		conn:    conn,
		picChan: make(chan *[]byte, 2),
		ctx:     ctx,
	}
	newConnChan <- c

	// read websocket
	go func() {
		defer func() {
			log.Println("read websocket go routine closed")
		}()
		for {
			select {
			case <-ctx.Done():
				log.Println("read websocket go routine closed from ctx.Done()")
				return
			default:
				msg, op, err := wsutil.ReadClientData(conn)
				if err != nil {
					switch err.(type) {
					case wsutil.ClosedError:
						cancel()
						return
					}
					switch err {
					case io.EOF:
						cancel()
					default:
						log.Println(err)
						raven.CaptureError(err, nil)
					}
					return
				} else {
					ravenMessage := fmt.Sprintf("msg: %#v, op: %#v, err: %#v", msg, op, err)
					raven.CaptureMessage(ravenMessage, nil)
				}
			}
		}
	}()

	// write websocket
	go func() {
		defer func() {
			c.conn.Close()
			close(c.picChan)
			log.Println("write websocket go routine closed")
		}()
		for {
			select {
			case <-ctx.Done():
				return
			case pic := <-c.picChan:
				if len(c.picChan) != 0 {
					log.Println("dropping frame to get more recent pic")
					continue
				}
				err := wsutil.WriteServerMessage(conn, ws.OpText, *pic)
				if err != nil {
					cancel()
					log.Println(err)
					switch err.(type) {
					case *net.OpError:
					default:
						raven.CaptureError(err, nil)
					}
					return
				}
				log.Println("served somebody")
			}
		}
	}()
}

func takePictures() {
	var streaming bool
	clients := map[net.Conn]client{}

	for {
		select {
		case c := <-newConnChan:
			clients[c.conn] = c
		default:
			if len(clients) == 0 {
				if streaming {
					err := camera.StopStreaming()
					if err != nil {
						log.Println(err)
						raven.CaptureError(err, nil)
					}
					streaming = false
				}
				time.Sleep(250 * time.Millisecond)
				continue
			}

			if !streaming {
				err := camera.StartStreaming()
				if err != nil {
					log.Println(err)
					continue
				}
				streaming = true
			}
			start := time.Now()
			err := camera.WaitForFrame(1)
			if err != nil {
				switch err.(type) {
				case *webcam.Timeout:
				default:
					log.Println(err)
					raven.CaptureError(err, nil)
				}
				continue
			}

			frame, err := camera.ReadFrame()
			if err != nil {
				log.Println(err)
				raven.CaptureError(err, nil)
				continue
			}

			base64image := make([]byte, base64.StdEncoding.EncodedLen(len(frame)))
			base64.StdEncoding.Encode(base64image, frame)

			for _, c := range clients {
				if c.ctx.Err() == nil {
					if len(c.picChan) != cap(c.picChan) {
						c.picChan <- &base64image
					} else {
						log.Println("there's one in the chamber")
					}
				} else {
					delete(clients, c.conn)
				}
			}

			log.Printf("%d bytes, captured image in %s", len(base64image), time.Since(start).String())
		}
	}
}

func openCamera() (err error) {
	camera, err = webcam.Open(webcamDevicePath)
	if err != nil {
		return
	}
	_, _, _, err = camera.SetImageFormat(webcam.PixelFormat(config.PixelFormat), uint32(config.Width), uint32(config.Height))
	if err != nil {
		return
	}
	err = camera.SetAutoWhiteBalance(true)
	return
}

func dumpWebcamFormats() {
	for pf, info := range camera.GetSupportedFormats() {
		log.Printf("\n\npixelFormat: %v %s, frame sizes:\n", pf, info)
		for _, size := range camera.GetSupportedFrameSizes(pf) {
			log.Printf("%#v\n", size)
		}
	}
}
