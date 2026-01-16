package main

import (
	"changeme/decrypt"
	"context"
	"encoding/json"
	"fmt"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	"io"
	"net/http"
	"strings"
	"time"
)

// App struct
type App struct {
	ctx context.Context
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{}
}

// startup is called at application startup
func (a *App) startup(ctx context.Context) {
	// Perform your setup here
	a.ctx = ctx

	// Set window size to 60% of primary screen
	screens, err := runtime.ScreenGetAll(ctx)
	if err == nil && len(screens) > 0 {
		var primaryScreen runtime.Screen
		foundPrimary := false
		for _, screen := range screens {
			if screen.IsPrimary {
				primaryScreen = screen
				foundPrimary = true
				break
			}
		}
		if !foundPrimary {
			primaryScreen = screens[0]
		}

		width := int(float64(primaryScreen.Size.Width) * 0.6)
		height := int(float64(primaryScreen.Size.Height) * 0.6)

		// Respect minimum size constraints
		if width < 800 {
			width = 800
		}
		if height < 600 {
			height = 600
		}

		runtime.WindowSetSize(ctx, width, height)
		runtime.WindowCenter(ctx)
	}
}

// domReady is called after front-end resources have been loaded
func (a App) domReady(ctx context.Context) {
	// Add your action here
}

// beforeClose is called when the application is about to quit,
// either by clicking the window close button or calling runtime.Quit.
// Returning true will cause the application to continue, false will continue shutdown as normal.
func (a *App) beforeClose(ctx context.Context) (prevent bool) {
	return false
}

// shutdown is called at application termination
func (a *App) shutdown(ctx context.Context) {
	// Perform your teardown here
}

// Greet returns a greeting for the given name
func (a *App) Greet(name string) string {
	return fmt.Sprintf("Hello %s, It's show time!", name)
}
func (a *App) GetLoginUid() string {
	type Res struct {
		Uid string `json:"uid"`
	}
	url := "https://weread.qq.com/web/login/getuid"
	client := http.Client{}
	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	var res Res
	err = json.NewDecoder(resp.Body).Decode(&res)
	if err != nil {
		return ""
	}
	return res.Uid
}
func (a *App) ConfirmLogin(uid string) string {
	type Res struct {
		Skey         string `json:"skey"`
		Vid          int    `json:"vid"`
		RedirectUri  string `json:"redirect_uri"`
		Pf           int    `json:"pf"`
		Code         string `json:"code"`
		ExpireMode   int    `json:"expireMode"`
		IsAutoLogout int    `json:"isAutoLogout"`
	}
	url := "https://weread.qq.com/web/login/getinfo"
	client := http.Client{
		Timeout: 60 * time.Second,
	}
	req, _ := http.NewRequest("POST", url, strings.NewReader(fmt.Sprintf(`{"uid":"%s"}`, uid)))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	var res Res
	err = json.NewDecoder(resp.Body).Decode(&res)
	if err != nil {
		return ""
	}
	retStr := fmt.Sprintf(`{"skey":"%s","vid":%d,"redirect_uri":"%s","pf":%d,"code":"%s","expireMode":%d,"isAutoLogout":%d}`, res.Skey, res.Vid, res.RedirectUri, res.Pf, res.Code, res.ExpireMode, res.IsAutoLogout)
	return retStr
}
func (a *App) GetBookShelf(vid, skey string) string {
	url := "https://weread.qq.com/web/shelf"
	client := http.Client{}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		fmt.Println("创建请求失败:", err)
		return "[]"
	}
	
	req.Header.Set("Cookie", fmt.Sprintf(`wr_skey=%s;wr_vid=%s;wr_pf=2;`, skey, vid))
	req.Header.Set("Host", "weread.qq.com")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Upgrade-Insecure-Requests", "1")
	req.Header.Set("User-Agent", " Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/121.0.0.0 Safari/537.36 Edg/121.0.0.0")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7")
	req.Header.Set("Referer", "https://weread.qq.com/")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en-GB;q=0.8,en;q=0.7,en-US;q=0.6")

	resp, err := client.Do(req)
	if err != nil {
		fmt.Println("请求失败:", err)
		return "[]"
	}
	defer resp.Body.Close()
	
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("读取响应失败:", err)
		return "[]"
	}

	dataStr := string(data)
	// 查找rawBooks的起始位置
	startIndex := strings.Index(dataStr, `"rawBooks":`)
	if startIndex == -1 {
		fmt.Println("未找到rawBooks数据")
		return "[]"
	}
	
	// 提取rawBooks数据
	startIndex += len(`"rawBooks":`)
	endIndex := strings.Index(dataStr[startIndex:], `,"loadingMore"`)
	if endIndex == -1 {
		// 如果没有loadingMore，查找"books":后面的结束位置
		endIndex = strings.Index(dataStr[startIndex:], `]`)
		if endIndex == -1 {
			fmt.Println("未找到数据结束位置")
			return "[]"
		}
		endIndex++ // 包含]字符
	}
	
	rawBooks := dataStr[startIndex : startIndex+endIndex]
	return rawBooks
}

func (a *App) Download(bookId, skey, vid string) string {
	return decrypt.DownloadBook(bookId, skey, vid)
}
