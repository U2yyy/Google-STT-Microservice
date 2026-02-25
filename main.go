package main

import (
	"context"
	"encoding/json"
	"fmt"
	"gabby-proxy/utils"
	"io"
	"log"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	speech "cloud.google.com/go/speech/apiv2"
	"cloud.google.com/go/speech/apiv2/speechpb"
	"github.com/gorilla/websocket"
	"golang.org/x/oauth2/google"
	"gopkg.in/natefinch/lumberjack.v2"
)

const (
	CodeSuccess             = 1000
	CodeSstTimeout          = 4001
	CodeFailedConnectGoogle = 4002
	CodeBufferOverflow      = 4003
	CodeStreamBroken        = 4004
	CodeGoogleConfigError   = 4005
	CodeUnknown             = 5000
)

func normalizeLanguageCode(lang string) string {
	cleanLang := strings.ToLower(strings.TrimSpace(strings.ReplaceAll(lang, "_", "-")))

	switch cleanLang {
	case "zh", "zh-cn", "zh-sg": // 简体中文
		return "cmn-Hans-CN"
	case "zh-tw", "zh-hk": // 繁体中文
		return "cmn-Hant-TW"

	case "en", "en-us":
		return "en-US"
	case "en-gb":
		return "en-GB"
	case "en-au":
		return "en-AU"
	case "en-in":
		return "en-IN"

	// --- 欧洲常用语 ---
	case "fr", "fr-fr":
		return "fr-FR"
	case "de", "de-de":
		return "de-DE"
	case "es", "es-es":
		return "es-ES"
	case "it", "it-it":
		return "it-IT"
	case "pt", "pt-br": // 葡萄牙语
		return "pt-BR"
	case "pt-pt":
		return "pt-PT"
	case "ru", "ru-ru":
		return "ru-RU"
	case "uk", "uk-ua":
		return "uk-UA"

	// --- 亚洲常用语 ---
	case "ja", "ja-jp":
		return "ja-JP"
	case "ko", "ko-kr":
		return "ko-KR"
	case "th", "th-th":
		return "th-TH"
	case "vi", "vi-vn":
		return "vi-VN"
	case "id", "in", "id-id":
		return "id-ID"
	case "ms", "ms-my":
		return "ms-MY"
	case "hi", "hi-in":
		return "hi-IN"

	// --- 中东语言 ---
	case "ar", "ar-sa": // 阿拉伯语 (默认沙特)
		return "ar-SA"
	case "fa", "fa-ir":
		return "fa-IR"
	case "he", "iw", "he-il": // 'iw' 是希伯来语旧代码
		return "he-IL"
	case "tr", "tr-tr":
		return "tr-TR"
	case "ur", "ur-pk":
		return "ur-PK"

	case "af", "af-za": // 南非荷兰语
		return "af-ZA"
	case "am", "am-et": // 阿姆哈拉语
		return "am-ET"
	case "az", "az-az": // 阿塞拜疆语
		return "az-AZ"
	case "be", "be-by": // 白俄罗斯语 (Google STT 可能支持有限，尝试标准码)
		return "be-BY" // *注意：需确认模型支持情况
	case "bg", "bg-bg": // 保加利亚语
		return "bg-BG"
	case "bn", "bn-bd": // 孟加拉语 (默认孟加拉国)
		return "bn-BD" // 或 bn-IN
	case "ca", "ca-es": // 加泰罗尼亚语
		return "ca-ES"
	case "cs", "cs-cz": // 捷克语
		return "cs-CZ"
	case "da", "da-dk": // 丹麦语
		return "da-DK"
	case "el", "el-gr": // 希腊语
		return "el-GR"
	case "et", "et-ee": // 爱沙尼亚语
		return "et-EE"
	case "eu", "eu-es": // 巴斯克语
		return "eu-ES"
	case "fi", "fi-fi": // 芬兰语
		return "fi-FI"
	case "fil", "tl", "fil-ph": // 菲律宾语 ('tl' 是 Tagalog)
		return "fil-PH"
	case "gl", "gl-es": // 加利西亚语
		return "gl-ES"
	case "hr", "hr-hr": // 克罗地亚语
		return "hr-HR"
	case "hu", "hu-hu": // 匈牙利语
		return "hu-HU"
	case "hy", "hy-am": // 亚美尼亚语
		return "hy-AM"
	case "is", "is-is": // 冰岛语
		return "is-IS"
	case "ka", "ka-ge": // 格鲁吉亚语
		return "ka-GE"
	case "kk", "kk-kz": // 哈萨克语
		return "kk-KZ"
	case "km", "km-kh": // 高棉语
		return "km-KH"
	case "kn", "kn-in": // 卡纳达语
		return "kn-IN"
	case "ky", "ky-kg": // 吉尔吉斯语 (注意：Google STT 支持可能有限)
		return "ky-KG"
	case "lo", "lo-la": // 老挝语
		return "lo-LA"
	case "lt", "lt-lt": // 立陶宛语
		return "lt-LT"
	case "lv", "lv-lv": // 拉脱维亚语
		return "lv-LV"
	case "mk", "mk-mk": // 马其顿语
		return "mk-MK"
	case "ml", "ml-in": // 马拉雅拉姆语
		return "ml-IN"
	case "mn", "mn-mn": // 蒙古语
		return "mn-MN"
	case "mr", "mr-in": // 马拉地语
		return "mr-IN"
	case "my", "my-mm": // 缅甸语
		return "my-MM"
	case "nb", "no": // 挪威语 (Bokmål)
		return "nb-NO"
	case "ne", "ne-np": // 尼泊尔语
		return "ne-NP"
	case "nl", "nl-nl": // 荷兰语
		return "nl-NL"
	case "pl", "pl-pl": // 波兰语
		return "pl-PL"
	case "rm": // 罗曼什语 Google STT 可能不支持，兜底到英语
		return "en-US" // *Fallback
	case "ro", "ro-ro": // 罗马尼亚语
		return "ro-RO"
	case "si", "si-lk": // 僧伽罗语
		return "si-LK"
	case "sk", "sk-sk": // 斯洛伐克语
		return "sk-SK"
	case "sl", "sl-si": // 斯洛文尼亚语
		return "sl-SI"
	case "sr", "sr-rs": // 塞尔维亚语
		return "sr-RS"
	case "sv", "sv-se": // 瑞典语
		return "sv-SE"
	case "sw", "sw-tz": // 斯瓦希里语
		return "sw-TZ" // 或 sw-KE
	case "ta", "ta-in": // 泰米尔语 (默认印度)
		return "ta-IN" // 也有 ta-SG, ta-LK, ta-MY
	case "te", "te-in": // 泰卢固语
		return "te-IN"
	case "uz", "uz-uz": // 乌兹别克语
		return "uz-UZ"
	case "zu", "zu-za": // 祖鲁语
		return "zu-ZA"

	default:
		// 如果是空，返回美式英语
		if cleanLang == "" || cleanLang == "null" {
			return "en-US"
		}
		return lang
	}
}

type ClientSignal struct {
	Status  int    `json:"status"`
	Content string `json:"content"`
}

type ClientParams struct {
	UserId  int    `json:"userId"`
	Token   string `json:"token"`
	AppName string `json:"appName"`
}

// WebSocket关闭控制帧
func fkClose(conn *websocket.Conn, code int, text string) {
	// 状态码 + 文本说明
	msg := websocket.FormatCloseMessage(code, text)

	// 发送控制帧
	err := conn.WriteControl(websocket.CloseMessage, msg, time.Now().Add(time.Second))

	if err != nil && err != websocket.ErrCloseSent {
		log.Printf("⚠️ WriteControl failed: %v", err)
	}
}

// 引入心跳
const (
	pongWait   = 60 * time.Second
	pingPeriod = 25 * time.Second
	writeWait  = 10 * time.Second
)

// 全局的Google Client
var (
	speechClient *speech.Client
	projectID    string
)

//// 并发的读写锁，似乎不能写全局，废弃
//var (
//	wg     sync.WaitGroup
//	lock   sync.Mutex   // 互斥锁
//	rwlock sync.RWMutex // 写锁
//)

// 全局注册账户
func initSpeechClient() {
	ctx := context.Background()

	creds, err := google.FindDefaultCredentials(ctx, "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		log.Fatalf("❌ cant find credentials: %v", err)
	}

	projectID = creds.ProjectID
	if projectID == "" {
		log.Fatal("❌ non project_id")
	}
	log.Printf("✅ get Google project ID: %s", projectID)

	client, err := speech.NewClient(ctx)

	if err != nil {
		log.Fatalf("Failed to create Google Cloud client: %v", err)
	}
	speechClient = client
}

// 升级Websocket
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

// websocket处理发来的文件流
func websocketHandler(w http.ResponseWriter, r *http.Request) {
	log.Println("WebSocket read start")
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// 互斥锁
	var mu sync.Mutex

	// 获取首帧时间戳
	timestampStr := r.Header.Get("Timestamp")
	if timestampStr == "" {
		http.Error(w, "Missing Timestamp", http.StatusUnauthorized)
		return
	}

	timestamp, err := strconv.ParseInt(timestampStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid Timestamp", http.StatusUnauthorized)
		return
	}

	// 允许前后 5 分钟误差
	now := time.Now().UnixMilli()
	if timestamp < now-300000 || timestamp > now+300000 {
		http.Error(w, "Request Expired", http.StatusUnauthorized)
		return
	}

	// 获取首帧计算后Authorization
	authorization := r.Header.Get("Authorization")

	if authorization == "" {
		http.Error(w, "Missing Authorization", http.StatusUnauthorized)
		return
	}

	// 计算密钥
	key := utils.GetAesKey(timestamp)

	decryptedBytes, err := utils.AesDecrypt(authorization, key)
	if err != nil {
		// 解密失败 = 鉴权失败
		fmt.Printf("Auth failed: %v\n", err)
		http.Error(w, "Unauthorized: Decryption failed", http.StatusUnauthorized)
		return
	}

	fmt.Printf("✅ Auth Success! Plaintext: %s\n", string(decryptedBytes))

	// 这里提取客户端传入字段，定义为lang
	lang := r.URL.Query().Get("lang")

	transformedLang := normalizeLanguageCode(lang)

	log.Printf("📥 accept new connection,lang set as: %s", transformedLang)

	var clientParams ClientParams

	err = json.Unmarshal(decryptedBytes, &clientParams)
	if err != nil {
		fmt.Println("json decode failed:", err)
		return
	}

	connLogger := slog.With(
		"userId", clientParams.UserId,
		"appName", clientParams.AppName,
		"remoteAddr", r.RemoteAddr,
		"lang", transformedLang,
	)

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		connLogger.Info("error happened in websocket connect", "error", err)
		return
	}
	defer func(conn *websocket.Conn) {
		if err := conn.Close(); err != nil {
			connLogger.Info("close WebSocket error, non-fatal", "error", err)
		}
	}(conn)

	if err := conn.SetReadDeadline(time.Now().Add(pongWait)); err != nil {
		return
	}

	conn.SetPongHandler(func(appData string) error {
		if err := conn.SetReadDeadline(time.Now().Add(pongWait)); err != nil {
			return err
		}
		return nil
	})

	go func() {
		ticker := time.NewTicker(pingPeriod)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if err := conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
					return
				}
				mu.Lock()
				err := conn.WriteMessage(websocket.PingMessage, nil)
				mu.Unlock()
				if err != nil {
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	connLogger.Info("websocket connected!")

	// 建立Channel
	audioChannel := make(chan []byte, 100)

	stopSignal := make(chan struct{})

	// 安全关闭stopSignal 这个channel
	var stopOnce sync.Once
	safeCloseStopSignal := func() {
		stopOnce.Do(func() {
			close(stopSignal)
		})
	}

	defer func() {
		close(audioChannel)
		safeCloseStopSignal()
	}()

	sstDone := make(chan struct{})

	go func() {
		realTimeSST(conn, audioChannel, ctx, transformedLang, &mu, stopSignal, connLogger)
		close(sstDone)
	}()

	// WebSocket 消息结构体
	type wsMessage struct {
		messageType int
		data        []byte
		err         error
	}

	msgChan := make(chan wsMessage)

	// 启动 goroutine 读取 WebSocket 消息
	go func() {
		defer close(msgChan)
		for {
			messageType, data, err := conn.ReadMessage()
			select {
			case msgChan <- wsMessage{messageType, data, err}:
				if err != nil {
					return // 出错后退出
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	// 文件创建操作，暂时注释掉
	//fileName := fmt.Sprintf("gabby_record_%d.pcm", time.Now().Unix())
	//
	//file, err := os.Create(fileName)
	//
	//if err != nil {
	//	fmt.Println("create file failed!", err)
	//	return
	//}
	//defer func(file *os.File) {
	//	if err := file.Close(); err != nil {
	//		log.Printf("close audio file error,non-fatal:%v", err)
	//	}
	//}(file)

	for {
		select {
		case <-sstDone:
			connLogger.Info("SST goroutine exited")
			return

		case msg, ok := <-msgChan:
			if !ok {
				connLogger.Info("WebSocket read channel closed")
				return
			}

			if msg.err != nil {
				connLogger.Info("websocket closed", "error", msg.err)
				return
			}

			if msg.messageType == websocket.BinaryMessage {
				// 文件写操作，暂时不需要，注释掉
				//fmt.Printf("get message! len: %d byte", len(msg.data))
				//_, err := file.Write(msg.data)
				//if err != nil {
				//	fmt.Println("file write error:", err)
				//	break
				//}
				//fmt.Printf(".")
				select {
				case audioChannel <- msg.data:
				case <-time.After(2 * time.Second):
					// 2 秒钟都塞不进 audioChannel，SST 协程已卡死
					connLogger.Info("❌ buffer overflowed，sst not available")
					mu.Lock()
					fkClose(conn, CodeBufferOverflow, "Overflow")
					mu.Unlock()
					// 不加这个的话websocket关的比发的还快
					time.Sleep(500 * time.Millisecond)
					return
				case <-ctx.Done():
					return
				}
			}

			// 处理客户端传来的消息
			if msg.messageType == websocket.TextMessage {
				var signal ClientSignal
				if json.Unmarshal(msg.data, &signal) == nil {
					if signal.Status == 1001 {
						connLogger.Info("🛑 Client requested stop")
						safeCloseStopSignal()
					}
				}
			}

		case <-ctx.Done():
			return
		}
	}
}

// sst主逻辑
func realTimeSST(conn *websocket.Conn, audioChannel <-chan []byte, ctx context.Context, lang string, mu *sync.Mutex, stopSignal <-chan struct{}, logger *slog.Logger) {
	stream, err := speechClient.StreamingRecognize(ctx)

	if err != nil {
		mu.Lock()
		fkClose(conn, CodeFailedConnectGoogle, "Google connection failed")
		mu.Unlock()
		logger.Info("failed to connect to Google", "error", err)
		// 延迟一秒给客户端反应时间，再关闭
		time.Sleep(500 * time.Millisecond)
		return
	}

	var finishedText strings.Builder // 已经定稿的全段文字
	var currentInterim string        // 当前正在变的中间文字

	recognizerPath := fmt.Sprintf("projects/%s/locations/global/recognizers/_", projectID)

	// 发送配置
	err = stream.Send(&speechpb.StreamingRecognizeRequest{
		Recognizer: recognizerPath,
		StreamingRequest: &speechpb.StreamingRecognizeRequest_StreamingConfig{
			StreamingConfig: &speechpb.StreamingRecognitionConfig{
				Config: &speechpb.RecognitionConfig{
					DecodingConfig: &speechpb.RecognitionConfig_ExplicitDecodingConfig{
						ExplicitDecodingConfig: &speechpb.ExplicitDecodingConfig{
							Encoding:          speechpb.ExplicitDecodingConfig_LINEAR16,
							SampleRateHertz:   defaultSampleRateHz,
							AudioChannelCount: 1,
						},
					},
					Model:         "long",
					LanguageCodes: []string{lang},
					Features: &speechpb.RecognitionFeatures{
						EnableAutomaticPunctuation: true,
					},
				},
				StreamingFeatures: &speechpb.StreamingRecognitionFeatures{
					InterimResults: true,
				},
			},
		},
	})

	if err != nil {
		fkClose(conn, CodeGoogleConfigError, "send Google StreamingRecognize config failed")
		logger.Info("failed to send Google StreamingRecognize config", "error", err)
		return
	}

	vadConfig := loadVADConfigFromEnv(logger)
	vad, err := newEnergyVAD(vadConfig)
	if err != nil {
		_ = stream.CloseSend()
		mu.Lock()
		fkClose(conn, CodeGoogleConfigError, "invalid VAD config")
		mu.Unlock()
		logger.Info("failed to initialize VAD", "error", err)
		return
	}

	forwarder := newAudioForwarder(stream, audioChannel, stopSignal, logger, vad)
	go forwarder.run(ctx)

	type StreamReceive struct {
		Res *speechpb.StreamingRecognizeResponse
		Err error
	}

	recvChan := make(chan StreamReceive)

	go func() {
		defer close(recvChan)
		for {
			resp, err := stream.Recv()
			recvChan <- StreamReceive{resp, err}
			if err != nil {
				logger.Info("receiving stream ended", "error", err)
				return
			}
		}
	}()

	isStopping := false

	// 用引用法消除刷屏问题
	signalCh := stopSignal

	for {
		var timeoutChan <-chan time.Time
		if isStopping {
			timeoutChan = time.After(2 * time.Second)
		}

		select {
		case <-signalCh:
			isStopping = true
			signalCh = nil
			logger.Info("⏳ Waiting for final Google response...")
			continue

		case <-timeoutChan:
			logger.Info("⏰ Timeout waiting for the last res from Google")
			mu.Lock()
			// 超时强制关闭
			fkClose(conn, CodeSstTimeout, "Timeout waiting for the last res from Googleout")
			mu.Unlock()
			return

		case res, ok := <-recvChan:
			if !ok {
				return
			}

			if res.Err != nil {
				mu.Lock()
				if res.Err == io.EOF {
					logger.Info("✅ Google EOF")
					msg := map[string]any{
						"transcript":   finishedText.String() + currentInterim,
						"isFinal":      true,
						"isProcessing": true,
					}
					_ = conn.WriteJSON(msg)
					fkClose(conn, CodeSuccess, "Connection closed successfully")
				} else {
					// 异常结束
					logger.Info("Google streaming error", "error", res.Err)
					fkClose(conn, CodeStreamBroken, "stream_broken")
				}
				mu.Unlock()
				return
			}
			hasUpdate := false
			for _, result := range res.Res.Results {
				if len(result.Alternatives) == 0 {
					continue
				}
				alt := result.Alternatives[0]
				hasUpdate = true

				if result.IsFinal {
					text := alt.Transcript
					if finishedText.Len() > 0 && !strings.HasPrefix(text, " ") {
						finishedText.WriteString(" ")
					}
					finishedText.WriteString(text)
					currentInterim = ""
				} else {
					currentInterim = alt.Transcript
				}
			}

			if hasUpdate {
				msg := map[string]any{
					"transcript":   finishedText.String() + currentInterim,
					"isFinal":      false,
					"isProcessing": true,
				}
				mu.Lock()
				_ = conn.WriteJSON(msg)
				mu.Unlock()
			}
		}
	}

}

func main() {
	fileLogger := &lumberjack.Logger{
		Filename:   "./logs/app.log", // 日志文件路径
		MaxSize:    10,               // 每个日志文件最大 10MB
		MaxBackups: 3,                // 保留最近 3 个文件
		MaxAge:     7,                // 保留最近 7 天
		Compress:   true,             // 是否压缩旧日志 (gzip)
	}

	// 同时输出到 文件 和 控制台
	multiWriter := io.MultiWriter(os.Stdout, fileLogger)

	logger := slog.New(slog.NewJSONHandler(multiWriter, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	// 设置为全局默认 logger
	slog.SetDefault(logger)

	initSpeechClient()
	http.HandleFunc("/stt", websocketHandler)
	fmt.Println("SST Proxy launched at :8080...")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
