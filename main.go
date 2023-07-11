package main

import (
	"fmt"
	"io"
	"io/ioutil"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"gopkg.in/yaml.v2"
)

type Config struct {
	Port                int    `yaml:"port"`
	LogTimestamp        bool   `yaml:"log_timestamp"`
	CustomHTMLHead      string `yaml:"custom_html_head"`
	CustomHTMLBody      string `yaml:"custom_html_body"`
	EnableXSSProtection bool   `yaml:"enable_xss_protection"`
}

type FileMetadata struct {
	ID       string `json:"id"`
	Filename string `json:"filename"`
}

const (
	uploadsDir  = "uploads"
	logFileName = "log.txt"
	configFile  = "config.yml"
)

var config Config

func main() {
	// 加载配置文件
	err := loadConfig()
	if err != nil {
		fmt.Println("无法加载配置文件：", err)
		return
	}

	// 创建存储目录
	if _, err := os.Stat(uploadsDir); os.IsNotExist(err) {
		err = os.Mkdir(uploadsDir, os.ModePerm)
		if err != nil {
			fmt.Println("无法创建上传目录：", err)
			return
		}
	}

	// 设置路由
	http.HandleFunc("/", indexHandler)
	http.HandleFunc("/upload", uploadHandler)
	http.HandleFunc("/list", listHandler)
	http.HandleFunc("/file/", fileHandler)

	// 启动服务器
	addr := fmt.Sprintf(":%d", config.Port)
	fmt.Println("服务器已启动，监听端口", addr, "...")
	err = http.ListenAndServe(addr, nil)
	if err != nil {
		fmt.Println("服务器启动失败：", err)
	}
}

func loadConfig() error {
	content, err := ioutil.ReadFile(configFile)
	if err != nil {
		return err
	}

	err = yaml.Unmarshal(content, &config)
	if err != nil {
		return err
	}

	return nil
}

func indexHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		fmt.Fprintf(w, `
<!DOCTYPE html>
<html>
<head>
    <title>文件上传</title>
    <style>
        %s
    </style>
</head>
<body>
    %s
    <h1>文件上传</h1>
    <form action="/upload" method="post" enctype="multipart/form-data">
        <input type="file" name="file" id="file">
        <input type="submit" value="上传">
    </form>
    <br>
    <a href="/list">查看文件列表</a>
</body>
</html>
`, config.CustomHTMLHead, config.CustomHTMLBody)
	} else {
		// 处理其他请求（POST，PUT，DELETE等）
		http.NotFound(w, r)
	}
}

func uploadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		// 解析文件
		file, handler, err := r.FormFile("file")
		if err != nil {
			fmt.Println("无法解析文件：", err)
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}
		defer file.Close()

		// 创建目标文件
		filename := handler.Filename
		fileExt := filepath.Ext(filename)
		fileID := generateUUID() + fileExt
		filePath := filepath.Join(uploadsDir, fileID)
		f, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE, 0666)
		if err != nil {
			fmt.Println("无法创建文件：", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		defer f.Close()

		// 将文件内容复制到目标文件
		_, err = io.Copy(f, file)
		if err != nil {
			fmt.Println("无法保存文件：", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		// 记录操作日志
		logMessage := filename
		if config.LogTimestamp {
			logMessage += fmt.Sprintf(" [%s]", time.Now().Format("2006-01-02 15:04:05"))
		}
		writeLog(logMessage)

		// 生成文件的直链URL
		fileURL := fmt.Sprintf("/file/%s", fileID)

		fmt.Fprintf(w, `
<!DOCTYPE html>
<html>
<head>
    <title>文件上传成功</title>
    <style>
        %s
    </style>
</head>
<body>
    %s
    <h1>文件上传成功</h1>
    <p>文件已上传！</p>
    <p><a href="%s">点击这里</a>查看文件列表。</p>
</body>
</html>
`, config.CustomHTMLHead, config.CustomHTMLBody, fileURL)
	} else {
		// 处理其他请求（GET，PUT，DELETE等）
		http.NotFound(w, r)
	}
}

func listHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		// 获取文件列表
		files, err := getFileList()
		if err != nil {
			fmt.Println("无法获取文件列表：", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		fmt.Fprintf(w, `
<!DOCTYPE html>
<html>
<head>
    <title>文件列表</title>
    <style>
        %s
        table {
            border-collapse: collapse;
            width: 100%;
        }
        th, td {
            text-align: left;
            padding: 8px;
            border-bottom: 1px solid #ddd;
        }
        tr:hover {
            background-color: #f5f5f5;
        }
        th {
            background-color: #4CAF50;
            color: white;
        }
    </style>
</head>
<body>
    %s
    <h1>文件列表</h1>
    <table>
        <tr>
            <th>ID</th>
            <th>文件名</th>
        </tr>
`)
		for _, file := range files {
			fmt.Fprintf(w, "<tr><td>%s</td><td>%s</td></tr>", file.ID, file.Filename)
		}
		fmt.Fprintf(w, `
    </table>
    <br>
    <a href="/">返回上传页面</a>
</body>
</html>
`)
	} else {
		// 处理其他请求（POST，PUT，DELETE等）
		http.NotFound(w, r)
	}
}

func fileHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		fileID := r.URL.Path[len("/file/"):]
		filePath := filepath.Join(uploadsDir, fileID)

		// 检查文件是否存在
		_, err := os.Stat(filePath)
		if os.IsNotExist(err) {
			http.NotFound(w, r)
			return
		}

		// 设置文件下载头
		filename := filepath.Base(filePath)
		w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=\"%s\"", filename))

		// 设置文件的Content-Type
		contentType := mime.TypeByExtension(filepath.Ext(filename))
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		w.Header().Set("Content-Type", contentType)

		http.ServeFile(w, r, filePath)
	} else {
		// 处理其他请求（POST，PUT，DELETE等）
		http.NotFound(w, r)
	}
}

func writeLog(message string) {
	file, err := os.OpenFile(logFileName, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		fmt.Println("无法写入日志文件：", err)
		return
	}
	defer file.Close()

	logMessage := fmt.Sprintf("%s\n", message)
	_, err = file.WriteString(logMessage)
	if err != nil {
		fmt.Println("无法写入日志文件：", err)
	}
}

func generateUUID() string {
	uuidObj, err := uuid.NewRandom()
	if err != nil {
		fmt.Println("无法生成UUID：", err)
		return ""
	}
	return uuidObj.String()
}

func getFileList() ([]FileMetadata, error) {
	var files []FileMetadata

	err := filepath.Walk(uploadsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() {
			relPath, err := filepath.Rel(uploadsDir, path)
			if err != nil {
				return err
			}
			file := FileMetadata{
				ID:       relPath,
				Filename: filepath.Base(relPath),
			}
			files = append(files, file)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return files, nil
}
