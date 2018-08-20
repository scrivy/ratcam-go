package main

import (
	"context"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"

	"github.com/blackjack/webcam"
	"github.com/getsentry/raven-go"
	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
	"gopkg.in/yaml.v2"
)

const webcamDevicePath = "/dev/video0"

var newConnChan = make(chan client, 10)

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
		picChan: make(chan *[]byte, 2),
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
				err := wsutil.WriteServerBinary(conn, *pic)
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

func takePictures() {
	var streaming bool
	clients := map[net.Conn]client{}

	// read and parse config
	rawConfig, err := ioutil.ReadFile("config.yaml")
	if err != nil {
		panic(err)
	}
	var config Config
	err = yaml.Unmarshal(rawConfig, &config)
	if err != nil {
		panic(err)
	}
	log.Printf("config:\n%#v\n", config)

	// open camera
	camera, err := webcam.Open(webcamDevicePath)
	if err != nil {
		panic(err)
	}
	// dump supported output formats
	for pf, info := range camera.GetSupportedFormats() {
		log.Printf("\n\npixelFormat: %v %s, frame sizes:\n", pf, info)
		for _, size := range camera.GetSupportedFrameSizes(pf) {
			log.Printf("%#v\n", size)
		}
	}
	// set output format
	_, _, _, err = camera.SetImageFormat(webcam.PixelFormat(config.PixelFormat), uint32(config.Width), uint32(config.Height))
	if err != nil {
		panic(err)
	}
	err = camera.SetAutoWhiteBalance(true)
	if err != nil {
		panic(err)
	}

	for {
		if len(clients) == 0 {
			if streaming {
				err := camera.StopStreaming()
				if err != nil {
					log.Println(err)
					raven.CaptureError(err, nil)
				}
				streaming = false
			}

			// wait for a new connection
			c := <-newConnChan
			clients[c.conn] = c
		}

		if !streaming {
			err := camera.StartStreaming()
			if err != nil {
				log.Println(err)
				continue
			}
			streaming = true
		}
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

		for _, c := range clients {
			if c.ctx.Err() == nil {
				if len(c.picChan) != cap(c.picChan) {
					c.picChan <- &frame
				}
			} else {
				delete(clients, c.conn)
			}
		}
	}
}
