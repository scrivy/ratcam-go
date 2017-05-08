package main

import (
	"bytes"
	"io/ioutil"
	"log"
	"net/http"
	"os/exec"
	"time"
)

func main() {
	index, err := ioutil.ReadFile("index.html")
	if err != nil {
		log.Println(err)
		log.Fatalln("problem loading index.html")
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			http.Error(w, http.StatusText(405), 405)
			return
		}

		w.Header().Set("Cache-Control", "max-age=600")
		w.Write(index)
	})

	http.HandleFunc("/latest.jpg", func(w http.ResponseWriter, r *http.Request) {

		var imageBytes bytes.Buffer
		start := time.Now()
		cmd := exec.Command("fswebcam", "-r", "640x480", "--jpeg", "75", "-")
		cmd.Stdout = &imageBytes
		err := cmd.Run()
		if err != nil {
			log.Println(err)
			return
		}
		elapsed := time.Since(start)
		log.Printf("captured image in %s", elapsed)

		w.Write(imageBytes.Bytes())
	})

	log.Println("Listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
