package ws

import (
	"encoding/json"
	"sync"

	"github.com/gorilla/websocket"
)

type WSConn struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

func NewWSConn(conn *websocket.Conn) *WSConn {
	return &WSConn{conn: conn}
}

func (w *WSConn) Send(msg []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.conn.WriteMessage(websocket.TextMessage, msg)
}

func (w *WSConn) SendJSON(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.conn.WriteMessage(websocket.TextMessage, data)
}

func (w *WSConn) ReadJSON(v any) error {
	return w.conn.ReadJSON(v)
}

func (w *WSConn) Close() error {
	return w.conn.Close()
}
