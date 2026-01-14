package decrypt

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"io/ioutil"
	"net/http"
	"os"
	"os/user"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/bmaupin/go-epub"
	"golang.org/x/net/html"

	"github.com/yeka/zip"
)

func initHeader(req *http.Request, vid, skey string) *http.Request {
	req.Header["accessToken"] = []string{skey}
	req.Header["vid"] = []string{vid}
	req.Header["baseapi"] = []string{"34"}
	req.Header["appver"] = []string{"7.5.0.10162554"}
	req.Header["User-Agent"] = []string{"WeRead/7.5.0 WRBrand/xiaomi Dalvik/2.1.0 (Linux; U; Android 14; 2304FPN6DC Build/UKQ1.230804.001)"}
	req.Header["osver"] = []string{"14"}
	req.Header["channelId"] = []string{"12"}
	req.Header["basever"] = []string{"7.5.0.10162554"}
	req.Header["Connection"] = []string{"Keep-Alive"}
	return req
}

func getDownloadPath() (string, error) {
	usr, err := user.Current()
	if err != nil {
		return "", err
	}
	return filepath.Join(usr.HomeDir, "Documents", "WereadBooks"), nil
}

// 递归删除指定目录下的所有zip文件
func removeAllZipFiles(dirPath string) error {
	return filepath.Walk(dirPath, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(strings.ToLower(info.Name()), ".zip") {
			if err := os.Remove(path); err != nil {
				return err
			}
			fmt.Printf("已删除压缩文件: %s\n", path)
		}
		return nil
	})
}

func getKeyAndIV(vid int) ([]byte, []byte) {

	remapArr := [10]byte{0x2d, 0x50, 0x56, 0xd7, 0x72, 0x53, 0xbf, 0x22, 0xfb, 0x20}
	vidLen := len(strconv.Itoa(vid))
	vidRemap := make([]byte, vidLen)
	for i := 0; i < vidLen; i++ {
		vidRemap[i] = remapArr[strconv.Itoa(vid)[i]-'0']
	}
	key := make([]byte, 32)
	for i := 0; i < 32; i++ {
		key[i] = vidRemap[i%vidLen]
	}
	fmt.Println(key)
	key = key[0:16]
	iv := key[16:32]
	return key, iv
}

func getPassword(vid string, encryptKey string) string {
	vidInt, _ := strconv.Atoi(vid)
	key, iv := getKeyAndIV(vidInt)
	fmt.Println(key, iv)
	encryptData, _ := base64.StdEncoding.DecodeString(encryptKey)

	block, err := aes.NewCipher(key)
	if err != nil {
		fmt.Println(err)
		return ""
	}
	blockMode := cipher.NewCBCDecrypter(block, iv)
	decryptedData := make([]byte, 16)
	blockMode.CryptBlocks(decryptedData, encryptData)
	pwdStr := ""
	for i := 0; i < len(decryptedData); i++ {
		if decryptedData[i] < 32 || decryptedData[i] > 126 {
			continue
		}
		pwdStr += string(decryptedData[i])
	}
	fmt.Println("pwd", pwdStr)
	return pwdStr
}

func MergeTxtBook(bookName, bookPath string) {
	type BookInfo struct {
		BookId            string `json:"bookId"`
		Synckey           int    `json:"synckey"`
		ChapterUpdateTime int    `json:"chapterUpdateTime"`
		Chapters          []struct {
			ChapterUid  int     `json:"chapterUid"`
			ChapterIdx  int     `json:"chapterIdx"`
			UpdateTime  int     `json:"updateTime"`
			Title       string  `json:"title"`
			WordCount   int     `json:"wordCount"`
			Price       float64 `json:"price"`
			IsMPChapter int     `json:"isMPChapter"`
			Paid        int     `json:"paid"`
		} `json:"chapters"`
	}
	f, err := os.Open(filepath.Join(bookPath, "info.txt"))
	if err != nil {
		fmt.Println("打开info.txt失败:", err)
		return
	}
	defer f.Close()
	var bookInfo BookInfo
	err = json.NewDecoder(f).Decode(&bookInfo)
	if err != nil {
		fmt.Println("解析info.txt失败:", err)
		return
	}
	//直接在书籍文件夹中创建txt文件，不再使用"看这里"子文件夹
	txtPath := filepath.Join(bookPath, bookName+".txt")
	bookFile, err := os.Create(txtPath)
	if err != nil {
		fmt.Println("创建txt文件失败:", err)
		return
	}
	defer bookFile.Close()
	//读取章节信息
	for _, chapter := range bookInfo.Chapters {
		oldName := fmt.Sprintf("%s_%d_o", bookInfo.BookId, chapter.ChapterUid)
		newName := fmt.Sprintf("第%d章 %s", chapter.ChapterIdx, chapter.Title)
		//读取章节内容
		chapterPath := filepath.Join(bookPath, oldName)
		chapterFile, err := os.Open(chapterPath)
		if err != nil {
			fmt.Println("打开章节文件失败:", err, "路径:", chapterPath)
			continue // 跳过失败的章节，而不是panic
		}

		//写入章节内容
		_, err = bookFile.WriteString(newName + "\n\n\n")
		if err != nil {
			fmt.Println("写入章节标题失败:", err)
			chapterFile.Close()
			continue
		}
		buf := make([]byte, 1024)
		for {
			n, err := chapterFile.Read(buf)
			if err != nil {
				break
			}
			_, err = bookFile.Write(buf[:n])
			if err != nil {
				fmt.Println("写入章节内容失败:", err)
				break
			}
		}
		chapterFile.Close()
		_, err = bookFile.WriteString("\n\n\n")
		if err != nil {
			fmt.Println("写入章节分隔符失败:", err)
		}
	}

}

func MergePdfBook(bookName, bookPath string) {
	type BookInfo struct {
		BookId            string `json:"bookId"`
		Synckey           int    `json:"synckey"`
		ChapterUpdateTime int    `json:"chapterUpdateTime"`
		Chapters          []struct {
			ChapterUid  int      `json:"chapterUid"`
			ChapterIdx  int      `json:"chapterIdx"`
			UpdateTime  int      `json:"updateTime"`
			Title       string   `json:"title"`
			WordCount   int      `json:"wordCount"`
			Price       int      `json:"price"`
			IsMPChapter int      `json:"isMPChapter"`
			Paid        int      `json:"paid"`
			Level       int      `json:"level"`
			Files       []string `json:"files"`
			Anchors     []struct {
				Title  string `json:"title"`
				Anchor string `json:"anchor"`
				Level  int    `json:"level"`
			} `json:"anchors,omitempty"`
		} `json:"chapters"`
	}
	page := `<div style="page-break-after: always;"></div>`
	//css样式
	stylesPath := filepath.Join(bookPath, "Styles")
	styles, err := os.ReadDir(stylesPath)
	if err != nil {
		fmt.Println("读取Styles目录失败:", err)
		// 如果Styles目录不存在，继续处理
		styles = []fs.DirEntry{}
	}
	var styleBody string
	for _, style := range styles {
		s := fmt.Sprintf(`<link href="../Styles/%s" rel="stylesheet" type="text/css" />`+"\n", style.Name())
		styleBody += s
	}
	htmlBody := `
<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.1//EN" "http://www.w3.org/TR/xhtml11/DTD/xhtml11.dtd">

<html xmlns="http://www.w3.org/1999/xhtml">
  <head>
    <title></title>
    ` + styleBody + `
  </head>
	<body>
	  </body>
</html>
	`

	infoPath := filepath.Join(bookPath, "info.txt")
	f, err := os.Open(infoPath)
	if err != nil {
		fmt.Println("打开info.txt失败:", err)
		return
	}
	defer f.Close()
	var bookInfo BookInfo
	err = json.NewDecoder(f).Decode(&bookInfo)
	if err != nil {
		fmt.Println("解析info.txt失败:", err)
		return
	}

	// 使用第一个章节的文件作为封面
	if len(bookInfo.Chapters) > 0 && len(bookInfo.Chapters[0].Files) > 0 {
		coverPath := filepath.Join(bookPath, bookInfo.Chapters[0].Files[0])
		coverFile, err := os.Open(coverPath)
		if err != nil {
			fmt.Println("打开封面文件失败:", err)
			// 如果封面文件打不开，继续处理
		} else {
			defer coverFile.Close()
		}
	}

	htmlDoc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlBody))
	if err != nil {
		fmt.Println("创建HTML文档失败:", err)
		return
	}

	for _, chapter := range bookInfo.Chapters {
		for _, file := range chapter.Files {
			filePath := filepath.Join(bookPath, file)
			f, err := os.Open(filePath)
			if err != nil {
				fmt.Println("打开章节文件失败:", err, "路径:", filePath)
				continue
			}
			docBody, err := io.ReadAll(f)
			if err != nil {
				fmt.Println("读取章节文件失败:", err)
				f.Close()
				continue
			}
			f.Close() // 直接关闭文件
			docHtml := string(docBody)
			docHtml = strings.Split(docHtml, "</head>")[1]
			docHtml = strings.Split(docHtml, "</html>")[0]
			bodyData := strings.ReplaceAll(docHtml, "body", "div")
			htmlDoc.Find("body").AppendHtml(bodyData)
			htmlDoc.Find("body").AppendHtml(page)
		}
	}
	//直接在书籍文件夹中创建HTML文件，不再使用"看这里"子文件夹
	htmlPath := filepath.Join(bookPath, bookName+".html")
	bookFile, err := os.Create(htmlPath)
	if err != nil {
		fmt.Println("创建HTML文件失败:", err)
		return
	}
	defer bookFile.Close()
	//写入bookFile
	h, err := htmlDoc.Html()
	if err != nil {
		fmt.Println("获取HTML内容失败:", err)
		return
	}
	_, err = bookFile.WriteString(html.UnescapeString(h))
	if err != nil {
		fmt.Println("写入HTML文件失败:", err)
		return
	}
}

// 生成EPUB文件
func GenerateEPUB(bookName, bookPath string) {
	type BookInfo struct {
		BookId            string `json:"bookId"`
		Synckey           int    `json:"synckey"`
		ChapterUpdateTime int    `json:"chapterUpdateTime"`
		Chapters          []struct {
			ChapterUid  int      `json:"chapterUid"`
			ChapterIdx  int      `json:"chapterIdx"`
			UpdateTime  int      `json:"updateTime"`
			Title       string   `json:"title"`
			WordCount   int      `json:"wordCount"`
			Price       int      `json:"price"`
			IsMPChapter int      `json:"isMPChapter"`
			Paid        int      `json:"paid"`
			Level       int      `json:"level"`
			Files       []string `json:"files"`
			Anchors     []struct {
				Title  string `json:"title"`
				Anchor string `json:"anchor"`
				Level  int    `json:"level"`
			} `json:"anchors,omitempty"`
		} `json:"chapters"`
	}

	// 读取书籍信息
	infoPath := filepath.Join(bookPath, "info.txt")
	f, err := os.Open(infoPath)
	if err != nil {
		fmt.Println("打开info.txt失败:", err)
		return
	}
	defer f.Close()

	var bookInfo BookInfo
	err = json.NewDecoder(f).Decode(&bookInfo)
	if err != nil {
		fmt.Println("解析info.txt失败:", err)
		return
	}

	// 创建EPUB实例
	book := epub.NewEpub(bookName)

	// 添加内容到EPUB
	for _, chapter := range bookInfo.Chapters {
		// 章节标题
		chapterTitle := chapter.Title
		var chapterContent string

		// 读取章节内容
		for _, file := range chapter.Files {
			filePath := filepath.Join(bookPath, file)
			f, err := os.Open(filePath)
			if err != nil {
				fmt.Println("打开章节文件失败:", err, "路径:", filePath)
				continue
			}

			docBody, err := io.ReadAll(f)
			if err != nil {
				fmt.Println("读取章节文件失败:", err)
				f.Close()
				continue
			}
			f.Close()

			docHtml := string(docBody)
			// 提取body内容
			if strings.Contains(docHtml, "</head>") {
				docHtml = strings.Split(docHtml, "</head>")[1]
			}
			if strings.Contains(docHtml, "</html>") {
				docHtml = strings.Split(docHtml, "</html>")[0]
			}
			// 替换body标签为div
			bodyData := strings.ReplaceAll(docHtml, "body", "div")
			chapterContent += bodyData
		}

		// 添加章节到EPUB
		_, err := book.AddSection(chapterContent, chapterTitle, "", "")
		if err != nil {
			fmt.Println("添加章节到EPUB失败:", err)
			continue
		}
	}

	// 直接在书籍文件夹中生成EPUB文件，不再使用"看这里"子文件夹
	epubPath := filepath.Join(bookPath, bookName+".epub")

	// 写入EPUB文件
	err = book.Write(epubPath)
	if err != nil {
		fmt.Println("写入EPUB文件失败:", err)
		return
	}

	fmt.Printf("EPUB文件生成成功: %s\n", epubPath)
}

func GetBookInfo(bookId, skey, vid string) (int64, int64, string, string, string, string) {

	url := "https://i.weread.qq.com/book/info?bookId=" + bookId + "&myzy=1&source=reading&teenmode=0"
	client := http.Client{}
	fmt.Println("获取书籍信息请求Url", url)
	req, _ := http.NewRequest("GET", url, nil)
	req = initHeader(req, vid, skey)

	resp, err := client.Do(req)
	if err != nil {
		fmt.Println(err)
	}
	fmt.Println("获取书籍信息返回状态码", resp.StatusCode)

	defer resp.Body.Close()

	type Res struct {
		BookId         string  `json:"bookId"`
		Title          string  `json:"title"`
		Author         string  `json:"author"`
		Translator     string  `json:"translator"`
		Cover          string  `json:"cover"`
		Version        int64   `json:"version"`
		Format         string  `json:"format"`
		Type           int     `json:"type"`
		Price          float64 `json:"price"`
		OriginalPrice  int     `json:"originalPrice"`
		Soldout        int     `json:"soldout"`
		BookStatus     int     `json:"bookStatus"`
		PayType        int     `json:"payType"`
		Intro          string  `json:"intro"`
		CentPrice      int     `json:"centPrice"`
		Finished       int     `json:"finished"`
		MaxFreeChapter int     `json:"maxFreeChapter"`
		Free           int     `json:"free"`
		McardDiscount  int     `json:"mcardDiscount"`
		Ispub          int     `json:"ispub"`
		ExtraType      int     `json:"extra_type"`
		Cpid           int     `json:"cpid"`
		PublishTime    string  `json:"publishTime"`
		Category       string  `json:"category"`
		Categories     []struct {
			CategoryId    int    `json:"categoryId"`
			SubCategoryId int    `json:"subCategoryId"`
			CategoryType  int    `json:"categoryType"`
			Title         string `json:"title"`
		} `json:"categories"`
		HasLecture     int    `json:"hasLecture"`
		LPushName      string `json:"lPushName"`
		ShouldHideTTS  int    `json:"shouldHideTTS"`
		LastChapterIdx int64  `json:"lastChapterIdx"`
		PaperBook      struct {
			SkuId string `json:"skuId"`
		} `json:"paperBook"`
		BlockSaveImg          int     `json:"blockSaveImg"`
		Language              string  `json:"language"`
		HideUpdateTime        bool    `json:"hideUpdateTime"`
		IsEPUBComics          int     `json:"isEPUBComics"`
		PayingStatus          int     `json:"payingStatus"`
		ChapterSize           int64   `json:"chapterSize"`
		UpdateTime            int     `json:"updateTime"`
		OnTime                int     `json:"onTime"`
		LastChapterCreateTime int     `json:"lastChapterCreateTime"`
		UnitPrice             float64 `json:"unitPrice"`
		MarketType            int     `json:"marketType"`
		Isbn                  string  `json:"isbn"`
		Publisher             string  `json:"publisher"`
		TotalWords            int     `json:"totalWords"`
		PublishPrice          float64 `json:"publishPrice"`
		BookSize              int     `json:"bookSize"`
		Recommended           int     `json:"recommended"`
		LectureRecommended    int     `json:"lectureRecommended"`
		Follow                int     `json:"follow"`
		Secret                int     `json:"secret"`
		Offline               int     `json:"offline"`
		LectureOffline        int     `json:"lectureOffline"`
		FinishReading         int     `json:"finishReading"`
		HideReview            int     `json:"hideReview"`
		HideFriendMark        int     `json:"hideFriendMark"`
		Blacked               int     `json:"blacked"`
		IsAutoPay             int     `json:"isAutoPay"`
		Availables            int     `json:"availables"`
		Paid                  int     `json:"paid"`
		IsChapterPaid         int     `json:"isChapterPaid"`
		ShowLectureButton     int     `json:"showLectureButton"`
		Wxtts                 int     `json:"wxtts"`
		Myzy                  int     `json:"myzy"`
		MyzyPay               int     `json:"myzy_pay"`
		HasAuthorReview       int     `json:"hasAuthorReview"`
		Star                  int     `json:"star"`
		RatingCount           int     `json:"ratingCount"`
		RatingDetail          struct {
			One    int `json:"one"`
			Two    int `json:"two"`
			Three  int `json:"three"`
			Four   int `json:"four"`
			Five   int `json:"five"`
			Recent int `json:"recent"`
		} `json:"ratingDetail"`
		NewRating       int `json:"newRating"`
		NewRatingCount  int `json:"newRatingCount"`
		NewRatingDetail struct {
			Good     int    `json:"good"`
			Fair     int    `json:"fair"`
			Poor     int    `json:"poor"`
			Recent   int    `json:"recent"`
			MyRating string `json:"myRating"`
			Title    string `json:"title"`
		} `json:"newRatingDetail"`
		Ranklist struct {
			Seq           int    `json:"seq"`
			CategoryId    string `json:"categoryId"`
			CategoryName  string `json:"categoryName"`
			StoreSubType  int    `json:"storeSubType"`
			Scheme        string `json:"scheme"`
			RanklistCover struct {
				Tinycode                 string `json:"tinycode"`
				ChartTitle               string `json:"chart_title"`
				ChartDetailTitle         string `json:"chart_detail_title"`
				ChartDetailTitleDark     string `json:"chart_detail_title_dark"`
				ChartShareTitle          string `json:"chart_share_title"`
				ChartShareLogo           string `json:"chart_share_logo"`
				ChartBookDetialIcon      string `json:"chart_book_detial_icon"`
				ChartTag                 string `json:"chart_tag"`
				EinkChartTitle           string `json:"eink_chart_title"`
				ChartTitleMain           string `json:"chart_title_main"`
				ChartDetailTitleMain     string `json:"chart_detail_title_main"`
				ChartDetailTitleDarkMain string `json:"chart_detail_title_dark_main"`
				ChartBackgroundColor1    string `json:"chart_background_color_1"`
				ChartBackgroundColor2    string `json:"chart_background_color_2"`
				ChartTitleHeight         int    `json:"chart_title_height"`
				ChartTitleWidth          int    `json:"chart_title_width"`
				Desc                     string `json:"desc"`
			} `json:"ranklistCover"`
		} `json:"ranklist"`
		CopyrightInfo struct {
			Id      int    `json:"id"`
			Name    string `json:"name"`
			UserVid int    `json:"userVid"`
			Role    int    `json:"role"`
			Avatar  string `json:"avatar"`
		} `json:"copyrightInfo"`
		AuthorSeg []struct {
			Words     string `json:"words"`
			Highlight int    `json:"highlight"`
			AuthorId  string `json:"authorId"`
		} `json:"authorSeg"`
		TranslatorSeg []struct {
			Words     string `json:"words"`
			Highlight int    `json:"highlight"`
			AuthorId  string `json:"authorId"`
		} `json:"translatorSeg"`
		CoverBoxInfo struct {
			Blurhash string `json:"blurhash"`
			Colors   []struct {
				Key string `json:"key"`
				Hex string `json:"hex"`
			} `json:"colors"`
			DominateColor struct {
				Hex string    `json:"hex"`
				Hsv []float64 `json:"hsv"`
			} `json:"dominate_color"`
			CustomCover    string `json:"custom_cover"`
			CustomRecCover string `json:"custom_rec_cover"`
		} `json:"coverBoxInfo"`
	}

	var res Res
	err = json.NewDecoder(resp.Body).Decode(&res)
	if err != nil {
		fmt.Println(err, "json")
	}
	bookChapterSize := res.ChapterSize + res.LastChapterIdx - 1
	bookVersion := res.Version
	bookFormat := res.Format
	fmt.Println(bookChapterSize, bookVersion, bookFormat)
	return bookChapterSize, bookVersion, bookFormat, res.Title, res.Author, res.Publisher
}
func DownloadBook(bookId, skey, vid string) string {
	fmt.Println("开始下载", bookId)
	bookChapterSize, bookVersion, bookFormat, bookName, bookAuthor, bookPublisher := GetBookInfo(bookId, skey, vid)

	// 获取用户文档目录下的下载路径
	downloadsPath, err := getDownloadPath()
	if err != nil {
		fmt.Println("获取下载路径失败:", err)
		return "获取下载路径失败"
	}

	// 确保下载目录存在
	err = os.MkdirAll(downloadsPath, os.ModePerm)
	if err != nil {
		fmt.Println("创建下载目录失败:", err)
		return "创建下载目录失败"
	}

	url := fmt.Sprintf("https://i.weread.qq.com/book/chapterdownload?bookId=%s&chapters=0-%d&pf=wechat_wx-2001-android-100-weread&pfkey=pfKey&zoneId=1&bookVersion=%d&bookType=%s&quote=&release=1&stopAutoPayWhenBNE=1&preload=2&preview=0&offline=0&giftPayingCard=0&enVersion=7.5.0&modernVersion=7.5.0&teenmode=0", bookId, bookChapterSize, bookVersion, bookFormat)
	client := http.Client{}
	fmt.Println("下载请求Url", url)
	req, err := http.NewRequest("GET", url, nil)
	req = initHeader(req, vid, skey)
	resp, err := client.Do(req)
	if err != nil {
		fmt.Println(err, "do request")
		return "请求失败"
	}
	defer resp.Body.Close()
	bookData, _ := io.ReadAll(resp.Body)
	fmt.Println("下载请求返回状态码", resp.StatusCode)
	if resp.StatusCode == 401 {
		fmt.Println("下载请求返回body", string(bookData))
		return "登录超时,请清除登录数据，重新登录"
	}
	if resp.StatusCode == 402 {
		fmt.Println("下载请求返回body", string(bookData))
		return "没有下载整本书的权限，请检查是否在微信读书中购买了整本书"
	}

	//读取body
	encryptKey := resp.Header.Get("encryptKey")
	fmt.Println(vid, encryptKey)
	pwdStr := getPassword(vid, encryptKey)

	// 使用用户文档目录
	bookDir := filepath.Join(downloadsPath, vid)
	err = os.MkdirAll(bookDir, os.ModePerm)
	if err != nil {
		fmt.Println(err, "create book dir")
		return "创建书籍目录失败"
	}

	// 构建包含作者和出版社的完整书籍名称
	fullBookName := bookName
	if bookAuthor != "" {
		fullBookName += " - " + bookAuthor
	}
	if bookPublisher != "" {
		fullBookName += " - " + bookPublisher
	}

	zipPath := filepath.Join(bookDir, fullBookName+".zip")
	f, err := os.Create(zipPath)
	if err != nil {
		fmt.Println(err, "create f")
		return "创建文件失败"
	}
	defer f.Close()
	//写出文件
	_, err = f.Write(bookData)
	if err != nil {
		fmt.Println(err, "write file")
		return "写出文件失败"
	}
	//解压文件
	zipReader, err := zip.NewReader(bytes.NewReader(bookData), int64(len(bookData)))
	if err != nil {
		fmt.Println(err, "new zip reader")
		return "解压文件失败"
	}
	for _, f := range zipReader.File {
		if f.IsEncrypted() {
			f.SetPassword(pwdStr)
		}
		r, err := f.Open()
		if err != nil {
			fmt.Println(err, "open file")
			return "打开文件失败"
		}
		fileName := filepath.Join(bookDir, fullBookName, f.Name)
		_, err = os.Stat(fileName)
		if err == nil {
			continue
		}
		dir := path.Dir(fileName)
		_, err = os.Stat(dir)
		if err != nil {
			err = os.MkdirAll(dir, 0777)
			if err != nil {
				fmt.Println(err)
				return "创建文件夹失败"

			}
		}

		file, err := os.Create(fileName)
		if err != nil {
			fmt.Println(err)
			return "创建文件失败"
		}
		b, err := ioutil.ReadAll(r)
		if err != nil {
			fmt.Println(err)
			return "读取文件失败"
		}
		file.Write(b)
		file.Close()

	}
	// 删除原始加密ZIP文件，只保留解压后的文件夹
	if err := os.Remove(zipPath); err != nil {
		fmt.Println("删除ZIP文件失败:", err)
	}
	// 导出书籍
	bookPath := filepath.Join(bookDir, fullBookName+"/")
	if bookFormat == "epub" {
		MergePdfBook(bookName, bookPath)
	} else {
		MergeTxtBook(bookName, bookPath)
	}

	// 生成EPUB文件
	GenerateEPUB(bookName, bookPath)

	// 删除书籍文件夹中的所有压缩文件
	if err := removeAllZipFiles(bookPath); err != nil {
		fmt.Println("删除书籍文件夹中的压缩文件失败:", err)
	}

	return "下载完成"
}
