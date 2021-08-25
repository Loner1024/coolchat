package serv

import (
	"context"
	"github.com/gobwas/ws"
	"github.com/rs/zerolog/log"
	"net"
	"net/http"
	"sync"
	"errors"
)

// Server is a websocket server
type Server struct {
	once sync.Once
	id string
	address string
	sync.Mutex
	// session list
	users map[string]net.Conn
}

// NewServer create a Server
func NewServer(id,address string) *Server {
	return &Server{
		id:      id,
		address: address,
		users:   make(map[string]net.Conn,100),
	}
}

// Start server
func (s *Server) Start() error {
	mux:=http.NewServeMux()
	log.Error()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Upgrade
		conn,_,_,err:=ws.UpgradeHTTP(r,w)
		if err != nil {
			conn.Close()
			return
		}
		// read userId
		user:=r.URL.Query().Get("user")
		if user == "" {
			conn.Close()
			return
		}
		// add to session
		old,ok:=s.addUser(user,conn)
		if ok {
			// close old connect
			old.Close()
		}
		log.Info().Msgf("user %s in",user)

		go func(user string,conn net.Conn) {
			// read msg
			err:=s.readloop(user,conn)
			if err!=nil{
				log.Err(err).Send()
			}
			// close connection and del user
			conn.Close()
			s.delUser(user)
			log.Info().Msgf("connection of %s closed",user)
		}(user,conn)
	})
	log.Info().Msg("start")
	return http.ListenAndServe(s.address,mux)
}

func (s *Server) addUser(user string,conn net.Conn) (net.Conn,bool) {
	s.Lock()
	defer s.Unlock()
	old,ok:=s.users[user] // return old connect
	s.users[user] = conn // cache
	return old,ok
}

func (s *Server) delUser(user string) {
	s.Lock()
	defer s.Unlock()
	delete(s.users,user)
}

// Shutdown shutdown
func (s *Server) Shutdown() {
	s.once.Do(func() {
		s.Lock()
		defer s.Unlock()
		for _,conn:=range s.users{
			conn.Close()
		}
	})
}

func (s *Server) readloop(user string,conn net.Conn) error {
	for {
		// read a one frame message from tcp buffering
		frame,err:=ws.ReadFrame(conn)
		if err != nil {
			return nil
		}
		if frame.Header.OpCode == ws.OpClose {
			return errors.New("remote side close the conn")
		}
		if frame.Header.Masked {
			// use mask decode data package
			ws.Cipher(frame.Payload,frame.Header.Mask,0)
		}
		// receive text frame
		if frame.Header.OpCode == ws.OpText {
			go s.handle(user,string(frame.Payload))
		}
	}
}

func (s *Server) handle(user, message string) {
	log.Info().Msgf("receive message %s from %s",message,user)
	s.Lock()
	defer s.Unlock()
	broadcast:=message + " -- FROM" + user
	for u,conn:=range s.users {
		// cannot send message to self
		if u == user {
			continue
		}
		log.Info().Msgf("send to %s : %s",u,broadcast)
		err:=s.writeText(conn,broadcast)
		if err != nil {
			log.Info().Msgf("write to %s failed, error: %s",user,err)
		}
	}
}

func (s *Server) writeText(conn net.Conn,message string) error {
	// create a text frame
	f:=ws.NewTextFrame([]byte(message))
	return ws.WriteFrame(conn,f)
}

func RunServerStart(ctx context.Context,opts *ServerStartOptions,version string) error {
	server:=NewServer(opts.id,opts.listen)
	defer server.Shutdown()
	return server.Start()
}
