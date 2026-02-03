package upload

import (
	"fmt"

	"github.com/gin-gonic/gin"
	sftpservice "github.com/pornlapatP/EV/internal/sftp"
)

func UploadHandler(sftpSvc *sftpservice.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		file, err := c.FormFile("file")
		if err != nil {
			c.JSON(400, gin.H{"error": "file required"})
			return
		}

		src, err := file.Open()
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		defer src.Close()

		path := "upload/" + file.Filename
		fmt.Println("Uploading to:", path)

		if err := sftpSvc.Upload(path, src); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}

		c.JSON(200, gin.H{
			"message": "uploaded",
			"path":    path,
		})
	}
}
