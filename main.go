package main

import (
	"fmt"
	"io"
	"log"
	"os"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	// "github.com/pornlapatP/EV/internal/user/handler"
	// "golang.org/x/telemetry/config"
)

func main() {
	// test
	// SFTP connection parameters
	host := "localhost"        // Replace with your SFTP host
	port := 22                 // Default SFTP port is 22
	user := "pat"              // Replace with your username
	password := "patdevops123" // Replace with your password

	// Configure the SSH client
	config := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			ssh.Password(password),
		},
		// InsecureIgnoreHostKey is used for simplicity in this example.
		// For production, you should verify the host key (e.g., using ssh.FixedHostKey, etc.).
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}
	// Connect to the SSH server
	addr := fmt.Sprintf("%s:%d", host, port)
	conn, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		log.Fatalf("Failed to connect to SSH server: %v", err)
	}
	defer conn.Close() // Ensure the SSH connection is closed

	// Open an SFTP session over the existing SSH connection
	sftpClient, err := sftp.NewClient(conn)
	if err != nil {
		log.Fatalf("Failed to open SFTP session: %v", err)
	}
	defer sftpClient.Close() // Ensure the SFTP session is closed

	fmt.Println("Successfully connected to SFTP server")

	// --- Example Operation: List directory contents ---
	fmt.Println("Listing directory contents:")
	files, err := sftpClient.ReadDir("/")
	if err != nil {
		log.Fatalf("Failed to list directory: %v", err)
	}

	for _, f := range files {
		fmt.Printf("- %s (%d bytes)\n", f.Name(), f.Size())
	}

	// --- Example Operation: Upload a file (uncomment to use) ---

	localFile, err := os.Open("local_file.txt")
	if err != nil {
		log.Fatalf("Failed to open local file: %v", err)
	}
	defer localFile.Close()

	remoteFile, err := sftpClient.Create("/home/pat/upload/file.txt")
	if err != nil {
		log.Fatalf("Failed to create remote file: %v", err)
	}
	defer remoteFile.Close()

	_, err = io.Copy(remoteFile, localFile)
	if err != nil {
		log.Fatalf("Failed to upload file: %v", err)
	}
	fmt.Println("File uploaded successfully")
}
