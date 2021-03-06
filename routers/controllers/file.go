package controllers

import (
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"home-cloud/models"
	"home-cloud/service"
	"home-cloud/utils"
	"io/ioutil"
	"net/http"
	"strings"
)

// UploadFiles upload files to the system
func UploadFiles(c *gin.Context) {
	form, err := c.MultipartForm()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": 1, "message": "Invalid Request"})
	}
	user := c.Value("user").(*models.User)

	files := form.File["file"]
	vDir := c.Value("vDir").([]string)

	var folder *models.File
	folder, err = service.GetFileOrFolderInfoByPath(vDir, user)
	if err != nil {
		var status int
		if errors.Is(err, service.ErrInvalidOrPermission) {
			status = http.StatusNotFound
		} else if errors.Is(err, service.ErrSystem) {
			status = http.StatusInternalServerError
		} else {
			status = http.StatusBadRequest
		}
		c.JSON(status, gin.H{"success": 1, "message": GetErrorMessage(err)})
		return
	}

	res := make(map[string]interface{})
	for _, file := range files {
		if len(file.Filename) == 0 || strings.ContainsAny(file.Filename, "/?*|<>:\\") {
			res[file.Filename] = gin.H{
				"result":  false,
				"message": "Invalid File Name",
			}
		} else {
			if err = service.UploadFile(file, user, folder, c); err != nil {
				res[file.Filename] = gin.H{
					"result":  false,
					"message": GetErrorMessage(err),
				}
			} else {
				res[file.Filename] = gin.H{
					"result": true,
				}
			}
		}
	}
	// If entering uploading process, success will be always 0 and each file result will be in files array
	c.JSON(http.StatusOK, gin.H{
		"success": 0,
		"files":   res,
	})
}

// GetFolder get children list in the folder
func GetFolder(c *gin.Context) {
	user := c.Value("user").(*models.User)
	vDir := c.Value("vDir").([]string)

	folder, err := service.GetFileOrFolderInfoByPath(vDir, user)
	if err != nil {
		var status int
		if errors.Is(err, service.ErrInvalidOrPermission) {
			status = http.StatusNotFound
		} else if errors.Is(err, service.ErrSystem) {
			status = http.StatusInternalServerError
		} else {
			status = http.StatusBadRequest
		}
		c.JSON(status, gin.H{"success": 1, "message": GetErrorMessage(err)})
		return
	}
	var files []*models.File

	files, err = service.GetFolder(folder, user)
	if err != nil {
		var status int
		if errors.Is(err, service.ErrInvalidOrPermission) {
			status = http.StatusNotFound
		} else if errors.Is(err, service.ErrSystem) {
			status = http.StatusInternalServerError
		} else {
			status = http.StatusBadRequest
		}
		c.JSON(status, gin.H{"success": 1, "message": GetErrorMessage(err)})
	} else {
		var resFiles = make([]gin.H, len(files))
		for i, v := range files {
			resFiles[i] = gin.H{
				"Name":      v.Name,
				"IsDir":     v.IsDir,
				"Position":  v.Position,
				"Size":      v.Size,
				"FileType":  v.FileType,
				"UpdatedAt": v.UpdatedAt,
				"CreatedAt": v.CreatedAt,
				"CreatorId": service.GetUserNameByID(v.CreatorId),
				"OwnerId":   service.GetUserNameByID(v.OwnerId),
				"Favorite":  v.Favorite,
			}
		}
		c.JSON(http.StatusOK, gin.H{"success": 0, "children": resFiles})
	}
}

// NewFileOrFolder create a file or folder in current folder
func NewFileOrFolder(c *gin.Context) {
	user := c.Value("user").(*models.User)
	vDir := c.Value("vDir").([]string)

	folder, err := service.GetFileOrFolderInfoByPath(vDir, user)
	if err != nil {
		var status int
		if errors.Is(err, service.ErrInvalidOrPermission) {
			status = http.StatusNotFound
		} else if errors.Is(err, service.ErrSystem) {
			status = http.StatusInternalServerError
		} else {
			status = http.StatusBadRequest
		}
		c.JSON(status, gin.H{"success": 1, "message": GetErrorMessage(err)})
		return
	}
	newName := c.PostForm("name")
	t := c.PostForm("type")
	if !(t == "file" || t == "folder") {
		c.JSON(http.StatusBadRequest, gin.H{"success": 1, "message": "Invalid Type"})
		return
	}
	if len(newName) == 0 || strings.ContainsAny(newName, "/?*|<>:\\") {
		c.JSON(http.StatusBadRequest, gin.H{"success": 1, "message": "Invalid Name"})
		return
	}
	err = service.NewFileOrFolder(folder, user, newName, t, c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": 1, "message": GetErrorMessage(err)})
	} else {
		c.JSON(http.StatusOK, gin.H{"success": 0})
	}
}

// GetFile download a file
func GetFile(c *gin.Context) {
	//This will only return error page in plain text because it may not be processed by axios
	user := c.Value("user").(*models.User)
	vDir := c.Value("vDir").([]string)

	file, err := service.GetFileOrFolderInfoByPath(vDir, user)
	if err != nil {
		if errors.Is(err, service.ErrInvalidOrPermission) {
			c.String(http.StatusNotFound, "404 Not Found")
		} else if errors.Is(err, service.ErrSystem) {
			c.String(http.StatusInternalServerError, "500 Internal Server Error")
		} else {
			c.String(http.StatusBadRequest, "400 Bad Request")
		}
		return
	}
	var dst string
	var filename string
	dst, filename, err = service.GetFile(file, user)
	if err != nil {
		if errors.Is(err, service.ErrInvalidOrPermission) {
			c.String(http.StatusNotFound, "404 Not Found")
		} else {
			c.String(http.StatusBadRequest, "400 Bad Request")
		}
		return
	}
	var f []byte
	if user.Encryption == 0 {
		f, err = ioutil.ReadFile(dst)
		if err != nil {
			utils.GetLogger().Errorf("Error when finding %s for %s", dst, file.Position)
			c.String(http.StatusInternalServerError, "500 Internal Server Error")
			return
		}
	} else {
		if user.Encryption < 0 || user.Encryption > 3 {
			c.String(http.StatusBadRequest, "400 Bad Request")
			return
		}
		f, err = service.GetFileEncrypted(dst, user, c)
		if err != nil {
			utils.GetLogger().Errorf("Error when finding and decrypting %s for %s", dst, file.Position)
			c.String(http.StatusInternalServerError, "500 Internal Server Error")
			return
		}
	}
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
	c.Header("Content-Length", fmt.Sprintf("%d", len(f)))
	c.Header("Content-Type", http.DetectContentType(f))
	_, err = c.Writer.Write(f)
	if err != nil {
		//Delete header for download
		c.Writer.Header().Del("Content-Disposition")
		c.Writer.Header().Del("Content-Length")
		c.Writer.Header().Del("Content-Type")
		utils.GetLogger().Errorf("Error when writing %s to response", dst)
		c.String(http.StatusInternalServerError, "500 Internal Server Error")
	}
}

// GetFileOrFolderInfoByPath get the file or folder info based on its path
func GetFileOrFolderInfoByPath(c *gin.Context) {
	user := c.Value("user").(*models.User)
	vDir := c.Value("vDir").([]string)
	file, err := service.GetFileOrFolderInfoByPath(vDir, user)
	if err != nil {
		var status int
		if errors.Is(err, service.ErrInvalidOrPermission) {
			status = http.StatusNotFound
		} else if errors.Is(err, service.ErrSystem) {
			status = http.StatusInternalServerError
		} else {
			status = http.StatusBadRequest
		}
		c.JSON(status, gin.H{"success": 1, "message": GetErrorMessage(err)})
	} else {
		resFolderInfo := gin.H{
			"Name":     file.Name,
			"Position": file.Position,
		}
		if file.IsDir == 1 {
			c.JSON(http.StatusOK, gin.H{"success": 0, "type": "folder", "root": file.ParentId == uuid.Nil, "info": resFolderInfo})
		} else {
			var folder *models.File
			folder, err = service.GetFileOrFolderInfoByID(file.ParentId, user)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"success": 1, "message": GetErrorMessage(err)})
			} else {
				resFileInfo := gin.H{
					"Name":      file.Name,
					"Position":  file.Position,
					"Size":      file.Size,
					"FileType":  file.FileType,
					"UpdatedAt": file.UpdatedAt,
					"CreatedAt": file.CreatedAt,
					"CreatorId": service.GetUserNameByID(file.CreatorId),
					"OwnerId":   service.GetUserNameByID(file.OwnerId),
					"Favorite":  file.Favorite,
				}
				resParentFolderInfo := gin.H{
					"Name":     folder.Name,
					"Position": folder.Position,
				}

				c.JSON(http.StatusOK, gin.H{"success": 0, "type": "file", "info": resFileInfo, "parent_root": file.ParentId == uuid.Nil, "parent_info": resParentFolderInfo})
			}
		}
	}

}

// DeleteFile delete a file or folder and its children in the system
func DeleteFile(c *gin.Context) {
	user := c.Value("user").(*models.User)
	vDir := c.Value("vDir").([]string)

	file, err := service.GetFileOrFolderInfoByPath(vDir, user)
	if err != nil {
		var status int
		if errors.Is(err, service.ErrInvalidOrPermission) {
			status = http.StatusNotFound
		} else if errors.Is(err, service.ErrSystem) {
			status = http.StatusInternalServerError
		} else {
			status = http.StatusBadRequest
		}
		c.JSON(status, gin.H{"success": 1, "message": GetErrorMessage(err)})
		return
	}

	err = service.DeleteFile(file, user)
	//Will not raise error after starting to delete files
	if err != nil {
		var status int
		if errors.Is(err, service.ErrInvalidOrPermission) {
			status = http.StatusNotFound
		} else if errors.Is(err, service.ErrSystem) {
			status = http.StatusInternalServerError
		} else {
			status = http.StatusBadRequest
		}
		c.JSON(status, gin.H{"success": 1, "message": GetErrorMessage(err)})
	} else {
		c.JSON(http.StatusOK, gin.H{"success": 0})
	}
}

// ToggleFavorite change the favorite status of a file or a folder
func ToggleFavorite(c *gin.Context) {
	user := c.Value("user").(*models.User)
	vDir := c.Value("vDir").([]string)

	file, err := service.GetFileOrFolderInfoByPath(vDir, user)
	if err != nil {
		var status int
		if errors.Is(err, service.ErrInvalidOrPermission) {
			status = http.StatusNotFound
		} else if errors.Is(err, service.ErrSystem) {
			status = http.StatusInternalServerError
		} else {
			status = http.StatusBadRequest
		}
		c.JSON(status, gin.H{"success": 1, "message": GetErrorMessage(err)})
		return
	}
	err = service.ChangeFavoriteStatus(file, user)
	if err != nil {
		var status int
		if errors.Is(err, service.ErrInvalidOrPermission) {
			status = http.StatusNotFound
		} else if errors.Is(err, service.ErrSystem) {
			status = http.StatusInternalServerError
		} else {
			status = http.StatusBadRequest
		}
		c.JSON(status, gin.H{"success": 1, "message": GetErrorMessage(err)})
	} else {
		c.JSON(http.StatusOK, gin.H{"success": 0})
	}
}

// GetFavorites get all the files and folders that are set favorite
func GetFavorites(c *gin.Context) {
	user := c.Value("user").(*models.User)

	files, err := service.GetFavorites(user)
	if err != nil {
		var status int
		if errors.Is(err, service.ErrInvalidOrPermission) {
			status = http.StatusNotFound
		} else if errors.Is(err, service.ErrSystem) {
			status = http.StatusInternalServerError
		} else {
			status = http.StatusBadRequest
		}
		c.JSON(status, gin.H{"success": 1, "message": GetErrorMessage(err)})
	} else {
		resFileInfo := make([]gin.H, len(files))
		for i, v := range files {
			resFileInfo[i] = gin.H{
				"Name":     v.Name,
				"Position": v.Position,
				"IsDir":    v.IsDir,
			}
		}
		c.JSON(http.StatusOK, gin.H{"success": 0, "favorites": resFileInfo})
	}
}

// SearchFiles search file or folder based on keyword
func SearchFiles(c *gin.Context) {
	user := c.Value("user").(*models.User)
	keyword := c.PostForm("keyword")
	if keyword == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": 1, "message": "Please input keyword"})
	}
	files, err := service.SearchFiles(user, keyword)
	if err != nil {
		var status int
		if errors.Is(err, service.ErrInvalidOrPermission) {
			status = http.StatusNotFound
		} else if errors.Is(err, service.ErrSystem) {
			status = http.StatusInternalServerError
		} else {
			status = http.StatusBadRequest
		}
		c.JSON(status, gin.H{"success": 1, "message": GetErrorMessage(err)})
	} else {
		resFileInfo := make([]gin.H, len(files))
		for i, v := range files {
			resFileInfo[i] = gin.H{
				"Name":     v.Name,
				"Position": v.Position,
				"IsDir":    v.IsDir,
			}
		}
		c.JSON(http.StatusOK, gin.H{"success": 0, "result": resFileInfo})
	}
}
