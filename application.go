package main

import (
	"io/ioutil"
	"log"
	"net/http"
	"sync"
	"time"

	"gocv.io/x/gocv"
)

var (
	latestPicture   []byte
	pictureMutex    = &sync.RWMutex{}
	lastRequest     = time.Now()
	lastRequestLock = &sync.RWMutex{}
)

func main() {
	index, err := ioutil.ReadFile("index.html")
	if err != nil {
		panic(err)
	}

	go takePictures()

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			http.Error(w, http.StatusText(405), 405)
			return
		}
		w.Write(index)
	})

	http.HandleFunc("/latest.jpeg", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		pictureMutex.RLock()
		w.Write(latestPicture)
		pictureMutex.RUnlock()

		lastRequestLock.Lock()
		lastRequest = time.Now()
		lastRequestLock.Unlock()

		log.Println("served somebody")
	})

	log.Println("Listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func takePictures() {
	//	var imageBytes, stdErr bytes.Buffer
	var start time.Time
	var ok bool
	var err error

	webcam, err := gocv.VideoCaptureDevice(0)
	if err != nil {
		panic(err)
	}

	img := gocv.NewMat()

	for {
		lastRequestLock.RLock()
		if !lastRequest.Add(5 * time.Second).After(time.Now()) {
			time.Sleep(500 * time.Millisecond)
			lastRequestLock.RUnlock()
			continue
		}
		lastRequestLock.RUnlock()

		start = time.Now()
		pictureMutex.Lock()
		ok = webcam.Read(img)
		if !ok {
			pictureMutex.Unlock()
			continue
		}
		if img.Empty() {
			log.Println("Empty image")
			pictureMutex.Unlock()
			continue
		}
		latestPicture, err = gocv.IMEncode(".jpg", img)
		pictureMutex.Unlock()
		log.Printf("captured image in %s", time.Since(start).String())
	}
}
