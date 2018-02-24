package main

import (
	"bytes"
	"compress/flate"
	"context"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	_ "net/http/pprof"
	"time"

	"github.com/blackjack/webcam"
	"github.com/getsentry/raven-go"
	//	"github.com/gobwas/httphead"
	"github.com/gobwas/ws"
	"gopkg.in/yaml.v2"
)

const webcamDevicePath = "/dev/video0"

var (
	config       Config
	newConnChan  = make(chan client, 10)
	httpUpgrader ws.HTTPUpgrader
)

type Config struct {
	PixelFormat int
	Width       int
	Height      int
}

type client struct {
	conn     net.Conn
	picChan  chan *[]byte
	ctx      context.Context
	compress bool
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
	headers := http.Header{}
	headers.Set("Sec-Websocket-Extensions", "permessage-deflate")

	log.Printf("client request headers: %#v\n", r.Header)

	conn, _, hs, err := httpUpgrader.Upgrade(r, w, headers)
	if err != nil {
		log.Println(err)
		raven.CaptureError(err, nil)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	log.Println("handshake protocol: ", hs.Protocol)
	for _, option := range hs.Extensions {
		log.Println("extension: ", option.String())
	}

	ctx, cancel := context.WithCancel(context.Background())
	c := client{
		conn:     conn,
		picChan:  make(chan *[]byte, 2),
		ctx:      ctx,
		compress: true,
	}
	newConnChan <- c

	// read websocket
	go func() {
		defer func() {
			cancel()
			log.Println("read websocket: go routine closed")
		}()
		for {
			select {
			case <-ctx.Done():
				return
			default:
				frame, err := ws.ReadFrame(conn)
				if err != nil {
					log.Println("read websocket: ws.ReadFrame: ", err)
					return
				}
				log.Printf("read websocket: frame headers: %#v\n", frame.Header)

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
		var flateWriter *flate.Writer
		var imageBuffer bytes.Buffer
		defer func() {
			cancel()
			c.conn.Close()
			flateWriter.Close()
			close(c.picChan)
			log.Println("write websocket: go routine closed")
		}()

		flateWriter, err := flate.NewWriter(&imageBuffer, flate.DefaultCompression)
		if err != nil {
			log.Println(err)
			raven.CaptureError(err, nil)
			return
		}

		for {
			select {
			case <-ctx.Done():
				return
			case pic := <-c.picChan:
				if len(c.picChan) != 0 {
					log.Println("dropping frame to get more recent pic")
					continue
				}

				_, err := flateWriter.Write(*pic)
				if err != nil {
					log.Println("write websocket error: flateWriter.Write: ", err)
					return
				}
				err = flateWriter.Close()
				if err != nil {
					log.Println("write websocket error: flateWriter.Close: ", err)
					return
				}

				payload := imageBuffer.Bytes()
				imageBuffer.Reset()
				flateWriter.Reset(&imageBuffer)

				log.Printf("last 4 bytes: %#v\n", payload[len(payload)-4:])

				frame := ws.NewFrame(ws.OpBinary, true, payload)
				frame.Header.Rsv = ws.Rsv(true, false, false)

				log.Printf("write websocket: headers: %#v\n", frame.Header)

				err = ws.WriteFrame(conn, frame)
				if err != nil {
					log.Println("write websocket error: ws.WriteFrame: ", err)
					return
				}

				log.Printf("write websocket: before compress %d, wrote %d compressed bytes\n", len(*pic), len(payload))

				/*
						err = ws.WriteHeader(conn, ws.Header{
							Fin:    true,
							Rsv:    ws.Rsv(true, false, false),
							OpCode: ws.OpText,
							Length: len(payload),
							Masked: false,
						})
						if err != nil {
							log.Println("write websocket error: ws.WriteHeader: ", err)
							return
						}

						_, err = io.CopyN(conn, &imageBuffer, payloadLength)
						if err != nil {
							log.Println("write websocket error: io.CopyN: ", err)
							return
						}

					log.Printf("byte length: %d\n", imageBuffer.Len())

				*/
			}
		}
	}()
}

func takePictures() {
	var streaming bool
	clients := map[net.Conn]client{}

	camera, err := openCamera()
	if err != nil {
		panic(err)
	}
	defer camera.Close()
	dumpWebcamFormats(camera)

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
					} else {
						log.Println("there's one in the chamber")
					}
				} else {
					delete(clients, c.conn)
				}
			}
		}
	}
}

func openCamera() (camera *webcam.Webcam, err error) {
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

func dumpWebcamFormats(camera *webcam.Webcam) {
	for pf, info := range camera.GetSupportedFormats() {
		log.Printf("\n\npixelFormat: %v %s, frame sizes:\n", pf, info)
		for _, size := range camera.GetSupportedFrameSizes(pf) {
			log.Printf("%#v\n", size)
		}
	}
}
