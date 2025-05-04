package main

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins (you may restrict this in production)
	},
}

func wsHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		fmt.Println("Upgrade error:", err)
		return
	}
	defer conn.Close()

	fmt.Println("Client connected")

	for {
		msg := "Hello from Go server at " + time.Now().Format(time.RFC3339)
		err = conn.WriteMessage(websocket.TextMessage, []byte(msg))
		if err != nil {
			fmt.Println("Write error:", err)
			break
		}
		time.Sleep(3 * time.Second)
	}
}

func main() {
	http.HandleFunc("/ws", wsHandler)

	fmt.Println("Server started at :8080")
	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		panic("ListenAndServe: " + err.Error())
	}
}
