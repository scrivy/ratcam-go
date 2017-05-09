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
)

func main() {
	index, err := ioutil.ReadFile("index.html")
	if err != nil {
		log.Println(err)
		log.Fatalln("problem loading index.html")
	}

	go takePictures()

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			http.Error(w, http.StatusText(405), 405)
			return
		}

		w.Header().Set("Cache-Control", "max-age=600")
		w.Write(index)
	})

	http.HandleFunc("/latest.jpeg", func(w http.ResponseWriter, r *http.Request) {
		picBytes, err := getLatestPicture()
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(picBytes)
	})

	log.Println("Listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

var (
	latestPicture []byte
	mutex         *sync.RWMutex = &sync.RWMutex{}
)

func takePictures() {
	var imageBytes, stdErr bytes.Buffer
	var start time.Time
	for {
		start = time.Now()
		cmd := exec.Command("fswebcam", "-r", "1280x720", "--jpeg", "90", "-q", "-")
		cmd.Stdout = &imageBytes
		cmd.Stderr = &stdErr
		err := cmd.Run()
		if err != nil {
			log.Println(err)
			continue
		}
		elapsed := time.Since(start)
		log.Printf("captured image in %s", elapsed)
		fmt.Printf("\n%s\n", stdErr.String())
		if strings.Contains(stdErr.String(), "unrecoverable error") {
			imageBytes.Reset()
			stdErr.Reset()
			continue
		}
		mutex.Lock()
		latestPicture = imageBytes.Bytes()
		mutex.Unlock()
		imageBytes.Reset()
		stdErr.Reset()
	}

}

func getLatestPicture() (picture []byte, err error) {
	mutex.RLock()
	picture = latestPicture
	mutex.RUnlock()
	if len(picture) == 0 {
		err = errors.New("bad gorilla")
	}
	return
}
