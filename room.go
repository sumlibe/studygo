package main

import (
	"log"
	"net/http"

	"github.com/gorilla/websocket"
	"github.com/sumlibe/gochat/trace"
)

type room struct {
	// forwardは他のクライアントに転送するためのメッセージを保持するチャネルです
	forward chan []byte
	// joinはチャットルームに参加しようとしているクライアントのためのチャネルです
	join chan *client
	// leaveはチャットルームから退室しようとしているクライアントのためのチャネルです
	leave chan *client
	// clientsには在室しているすべてのクライアントが保持されます
	clients map[*client]bool
	// tracerはチャットルーム上で行なわれた操作のログを受け取ります
	tracer trace.Tracer
}

// newRoomはすぐに利用できるチャットルームを生成して返す
func newRoom() *room {
	return &room{
		forward: make(chan []byte),
		join:    make(chan *client),
		leave:   make(chan *client),
		clients: make(map[*client]bool),
		tracer:  trace.Off(),
	}
}

func (r *room) run() {
	for {
		select {
		case client := <-r.join:
			// 参加
			r.clients[client] = true
			r.tracer.Trace("新しいクライアントが参加しました")
		case client := <-r.leave:
			// 退室
			delete(r.clients, client)
			close(client.send)
			r.tracer.Trace("クライアントが退室しました")
		case msg := <-r.forward:
			r.tracer.Trace("メッセージを受信しました: ", string(msg))
			// すべてのクライアントにメッセージを転送
			for client := range r.clients {
				select {
				case client.send <- msg:
					// メッセージを送信
					r.tracer.Trace(" -- クライアントに送信されました")
				default:
					// 送信に失敗
					delete(r.clients, client)
					close(client.send)
					r.tracer.Trace(" -- 送信に失敗しました。クライアントをクリーンアップします")
				}
			}
		}
	}
}

const (
	socketBufferSize  = 1024
	messageBufferSize = 256
)

var upgrader = &websocket.Upgrader{ReadBufferSize: socketBufferSize, WriteBufferSize: socketBufferSize}

func (r *room) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	// HTTP接続をアップグレードしてWebSocket接続を取得する
	socket, err := upgrader.Upgrade(w, req, nil)
	if err != nil {
		log.Fatal("ServeHTTP:", err)
		return
	}
	// クライアントを生成して現在のチャットルームのjoinチャネルに渡す
	client := &client{
		socket: socket,
		send:   make(chan []byte, messageBufferSize),
		room:   r,
	}
	r.join <- client
	// クライアントの終了時に退室の処理を行う
	defer func() { r.leave <- client }()
	// goroutineとしてクライアントのwriteメソッドを処理する
	go client.write()
	// メインのスレッドでクライアントのreadメソッドを処理して接続を保持し、終了を指示されるまで他の処理をブロックする
	client.read()
}
