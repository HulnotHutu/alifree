package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

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
				Handler:           srv.mux(),
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

func (s *server) mux() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", s.handlePage)
	mux.HandleFunc("GET /api/quota", s.handleQuota)
	mux.HandleFunc("POST /api/refresh", s.handleRefresh)
	return mux
}

func (s *server) handlePage(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(dashboardHTML())
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

func (s *server) handleQuota(w http.ResponseWriter, r *http.Request) {
	meta := &apiMeta{DB: s.store.DBPath, Credentials: LoadCredentials(s.envFile).Complete()}
	id, snap, err := s.store.Latest()
	if err != nil {
		writeJSON(w, http.StatusOK, apiEnvelope{
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
	writeJSON(w, http.StatusOK, apiEnvelope{OK: true, Data: snap, Meta: meta})
}

func (s *server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	creds := LoadCredentials(s.envFile)
	if !creds.Complete() {
		writeJSON(w, http.StatusOK, apiEnvelope{
			OK:    false,
			Error: fmt.Sprintf("缺少凭证，无法刷新。请在 %s 写入 ALIBABA_CLOUD_ACCESS_KEY_ID / ALIBABA_CLOUD_ACCESS_KEY_SECRET（见 README），重启本服务后再试。", s.envFile),
			Meta:  &apiMeta{DB: s.store.DBPath, Credentials: false},
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 240*time.Second)
	defer cancel()
	snap, err := bailian.NewClient(creds.AK, creds.SK, creds.STS).Snapshot(ctx)
	if err != nil {
		writeJSON(w, http.StatusOK, apiEnvelope{
			OK:    false,
			Error: "抓取失败：" + err.Error(),
			Meta:  &apiMeta{DB: s.store.DBPath, Credentials: true},
		})
		return
	}
	id, err := s.store.Save(snap)
	if err != nil {
		writeJSON(w, http.StatusOK, apiEnvelope{OK: false, Error: "保存失败：" + err.Error()})
		return
	}
	zero := 0.0
	writeJSON(w, http.StatusOK, apiEnvelope{
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

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = encodeJSON(w, v)
}

// encodeJSON 输出缩进、非 HTML 转义的 JSON。
func encodeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"ok": false, "error": msg})
}
