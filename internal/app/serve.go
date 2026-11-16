package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/spf13/cobra"

	webui "github.com/HulnotHutu/dashscope-call"
	"github.com/HulnotHutu/dashscope-call/internal/bailian"
	"github.com/HulnotHutu/dashscope-call/internal/store"
)

func newServeCmd() *cobra.Command {
	var (
		host    string
		port    int
		dbPath  string
		envFile string
		open    bool
	)
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "启动本地额度看板网页",
		Example: `  quota serve                 # http://127.0.0.1:8377
  quota serve --port 9000 --open`,
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := store.Open(dbPath)
			if err != nil {
				return err
			}
			defer st.Close()
			srv := &server{store: st, envFile: envFile}

			addr := host + ":" + fmt.Sprint(port)
			httpSrv := &http.Server{
				Addr:              addr,
				Handler:           srv.router(),
				ReadHeaderTimeout: 10 * time.Second,
			}

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			errCh := make(chan error, 1)
			go func() { errCh <- httpSrv.ListenAndServe() }()

			url := "http://" + addr + "/"
			fmt.Fprintf(cmd.ErrOrStderr(), "额度看板：%s（数据库 %s，Ctrl+C 退出）\n", url, dbPath)
			if open {
				_ = exec.Command("xdg-open", url).Start()
			}

			select {
			case err := <-errCh:
				return err
			case <-ctx.Done():
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				return httpSrv.Shutdown(shutdownCtx)
			}
		},
	}
	cmd.Flags().StringVar(&host, "host", "127.0.0.1", "监听地址（默认只绑定本机）")
	cmd.Flags().IntVar(&port, "port", 8377, "监听端口")
	cmd.Flags().StringVar(&dbPath, "db", DefaultDBPath(), "SQLite 库文件路径")
	cmd.Flags().StringVar(&envFile, "env-file", DefaultEnvFile(), "AK/SK 凭证文件")
	cmd.Flags().BoolVar(&open, "open", false, "启动后打开浏览器")
	return cmd
}

// server 是看板的 HTTP 服务；凭证只在服务端进程内使用，不会发给浏览器。
type server struct {
	store   *store.Store
	envFile string
}

// router 构建看板与测试台的 Gin 路由：React SPA（go:embed web-ui/dist）+
// /api 分组（额度查询、刷新、测试台推理代理）。
func (s *server) router() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(s.logRequests, gin.Recovery())

	api := r.Group("/api")
	api.GET("/quota", s.handleQuota)
	api.GET("/history", s.handleHistory)
	api.GET("/key-status", s.handleKeyStatus)
	api.POST("/refresh", s.handleRefresh)

	infer := api.Group("/infer")
	infer.POST("/chat", s.handleInferChat)
	infer.POST("/embeddings", s.handleInferEmbeddings)
	infer.POST("/imagegen", s.handleInferImageGen)
	infer.GET("/tasks/:id/events", s.handleTaskEvents)
	infer.POST("/tts", s.handleInferTTS)
	infer.POST("/rerank", s.handleInferRerank)

	// 其余路径全部交给 SPA：存在的静态文件直出，
	// /api 未匹配路径保持 JSON 404，其余回退 index.html 交给 react-router
	r.NoRoute(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api") || c.Request.Method != http.MethodGet {
			writeError(c, http.StatusNotFound, "not found")
			return
		}
		s.spa(c)
	})
	return r
}

// logRequests 记录每次请求的入口（debug）与出口（info，含状态码与耗时），
// 与 aihelpme 守护进程的请求日志保持同一风格。
func (s *server) logRequests(c *gin.Context) {
	start := time.Now()
	slog.Debug("request start", "method", c.Request.Method, "path", c.Request.URL.Path)
	c.Next()
	slog.Info("request done",
		"method", c.Request.Method,
		"path", c.Request.URL.Path,
		"status", c.Writer.Status(),
		"duration_ms", time.Since(start).Milliseconds(),
	)
}

// spa 托管 React 构建产物（go:embed web-ui/dist）：静态资源按路径直出，
// 其余路径回退 index.html，前端路由由 react-router 处理。
func (s *server) spa(c *gin.Context) {
	dist, err := webui.Dist()
	if err != nil {
		writeError(c, http.StatusInternalServerError, err.Error())
		return
	}
	p := path.Clean("/" + c.Request.URL.Path) // 规整路径，防目录穿越
	if f, openErr := dist.Open(strings.TrimPrefix(p, "/")); openErr == nil {
		f.Close()
		c.FileFromFS(p, http.FS(dist))
		return
	}
	index, readErr := fs.ReadFile(dist, "index.html")
	if readErr != nil {
		writeError(c, http.StatusInternalServerError, "前端产物缺失：请在 web-ui/ 运行 npm run build 后重新编译")
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "text/html; charset=utf-8", index)
}

// handleHistory 返回最近抓取记录，供历史趋势图使用。
func (s *server) handleHistory(c *gin.Context) {
	runs, err := s.store.History(200)
	if err != nil {
		writeError(c, http.StatusInternalServerError, err.Error())
		return
	}
	if runs == nil {
		runs = []store.RunInfo{}
	}
	writeJSON(c, http.StatusOK, runs)
}

// apiEnvelope 与看板前端的 JSON 契约。
type apiEnvelope struct {
	OK    bool              `json:"ok"`
	Error string            `json:"error,omitempty"`
	Data  *bailian.Snapshot `json:"data,omitempty"`
	Meta  *apiMeta          `json:"meta,omitempty"`
}

type apiMeta struct {
	DB          string   `json:"db"`
	SnapshotID  int64    `json:"snapshotId,omitempty"`
	AgeHours    *float64 `json:"ageHours,omitempty"`
	Credentials bool     `json:"credentials"`
}

func (s *server) handleQuota(c *gin.Context) {
	meta := &apiMeta{DB: s.store.DBPath, Credentials: LoadCredentials(s.envFile).Complete()}
	id, snap, err := s.store.Latest()
	if err != nil {
		writeJSON(c, http.StatusOK, apiEnvelope{
			OK:    false,
			Error: err.Error() + "。先运行 `quota fetch --save` 抓取一次，或配置凭证后在页面里刷新。",
			Meta:  meta,
		})
		return
	}
	meta.SnapshotID = id
	if age, ok := ageHours(snap.FetchedAt); ok {
		meta.AgeHours = &age
	}
	writeJSON(c, http.StatusOK, apiEnvelope{OK: true, Data: snap, Meta: meta})
}

func (s *server) handleRefresh(c *gin.Context) {
	creds := LoadCredentials(s.envFile)
	if !creds.Complete() {
		writeJSON(c, http.StatusOK, apiEnvelope{
			OK:    false,
			Error: fmt.Sprintf("缺少凭证，无法刷新。请在 %s 写入 ALIBABA_CLOUD_ACCESS_KEY_ID / ALIBABA_CLOUD_ACCESS_KEY_SECRET（见 README），保存后直接再点刷新即可，无需重启。", s.envFile),
			Meta:  &apiMeta{DB: s.store.DBPath, Credentials: false},
		})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 240*time.Second)
	defer cancel()
	snap, err := bailian.NewClient(creds.AK, creds.SK, creds.STS).Snapshot(ctx)
	if err != nil {
		writeJSON(c, http.StatusOK, apiEnvelope{
			OK:    false,
			Error: "抓取失败：" + err.Error(),
			Meta:  &apiMeta{DB: s.store.DBPath, Credentials: true},
		})
		return
	}
	id, err := s.store.Save(snap)
	if err != nil {
		writeJSON(c, http.StatusOK, apiEnvelope{OK: false, Error: "保存失败：" + err.Error()})
		return
	}
	zero := 0.0
	writeJSON(c, http.StatusOK, apiEnvelope{
		OK:   true,
		Data: snap,
		Meta: &apiMeta{DB: s.store.DBPath, SnapshotID: id, AgeHours: &zero, Credentials: true},
	})
}

func ageHours(fetchedAt string) (float64, bool) {
	t, err := time.Parse(time.RFC3339, fetchedAt)
	if err != nil {
		return 0, false
	}
	return time.Since(t).Hours(), true
}

// writeJSON 输出缩进、非 HTML 转义的 JSON（与原 net/http 版本同一线格式，
// 前端契约不变）。
func writeJSON(c *gin.Context, status int, v any) {
	c.Header("Cache-Control", "no-store")
	var buf bytes.Buffer
	if err := encodeJSON(&buf, v); err != nil {
		return
	}
	c.Data(status, "application/json; charset=utf-8", buf.Bytes())
}

// encodeJSON 输出缩进、非 HTML 转义的 JSON。
func encodeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func writeError(c *gin.Context, status int, msg string) {
	writeJSON(c, status, map[string]any{"ok": false, "error": msg})
}
