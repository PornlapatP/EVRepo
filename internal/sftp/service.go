package sftp

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

type Service struct {
	host string
	port string
	user string
	pass string
}

func New() *Service {
	return &Service{
		host: os.Getenv("SFTP_HOST"),
		port: os.Getenv("SFTP_PORT"),
		user: os.Getenv("SFTP_USER"),
		pass: os.Getenv("SFTP_PASSWORD"),
	}
}

func (s *Service) connect() (*sftp.Client, *ssh.Client, error) {
	cfg := &ssh.ClientConfig{
		User: s.user,
		Auth: []ssh.AuthMethod{
			ssh.Password(s.pass),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}

	addr := fmt.Sprintf("%s:%s", s.host, s.port)
	conn, err := ssh.Dial("tcp", addr, cfg)
	if err != nil {
		return nil, nil, err
	}

	client, err := sftp.NewClient(conn)
	if err != nil {
		conn.Close()
		return nil, nil, err
	}

	return client, conn, nil
}

func (s *Service) Upload(path string, data io.Reader) error {
	client, conn, err := s.connect()
	if err != nil {
		return err
	}
	defer conn.Close()
	defer client.Close()

	f, err := client.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(f, data)
	return err
}
