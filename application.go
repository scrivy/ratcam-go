package main

import (
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/getsentry/raven-go"
)

func main() {
	index, err := ioutil.ReadFile("index.html")
	if err != nil {
		logErr(err)
		log.Fatalln("problem loading index.html")
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

var (
	latestPicture   []byte
	pictureMutex    = &sync.RWMutex{}
	lastRequest     = time.Now()
	lastRequestLock = &sync.RWMutex{}
)

func takePictures() {
	var imageBytes, stdErr bytes.Buffer
	var start time.Time
	var err error
	for {
		lastRequestLock.RLock()
		if !lastRequest.Add(5 * time.Second).After(time.Now()) {
			time.Sleep(500 * time.Millisecond)
			lastRequestLock.RUnlock()
			continue
		}
		lastRequestLock.RUnlock()

		pictureMutex.Lock()
		start = time.Now()
		cmd := exec.Command("fswebcam", "-r", "1920x1080", "--jpeg", "90", "-q", "--no-banner", "-")
		cmd.Stdout = &imageBytes
		cmd.Stderr = &stdErr
		err = cmd.Run()
		if err != nil {
			logErr(err)
			continue
		}
		elapsed := time.Since(start)
		fmt.Printf("\n%s\n", stdErr.String())
		if strings.Contains(stdErr.String(), "unrecoverable error") {
			logErr(errors.New(stdErr.String()))
		} else if strings.Contains(stdErr.String(), "Error opening device") {
			logErr(errors.New(stdErr.String()))
		} else if strings.Contains(stdErr.String(), "No such file or directory") {
			logErr(errors.New(stdErr.String()))
		} else if len(stdErr.String()) > 0 {
			logErr(errors.New(stdErr.String()))
		} else {
			latestPicture = imageBytes.Bytes()
			log.Printf("captured image in %s", elapsed)
		}
		pictureMutex.Unlock()
		imageBytes.Reset()
		stdErr.Reset()
	}
}

func logErr(err error) {
	log.Println(err)
	raven.CaptureErrorAndWait(err, nil)
}
