package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"forum/internal/community"
	"forum/internal/config"
	"forum/internal/database"
	"forum/internal/post"
	"forum/internal/router"
	"forum/internal/session"
	"forum/internal/user"
	"forum/internal/vote"
	"forum/pkg/jwt"
	"forum/pkg/snowflake"

	"github.com/redis/go-redis/v9"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	cfg, err := config.Load("config/config.yaml")
	if err != nil {
		return err
	}
	tokens, err := jwt.New(cfg.JWT.Options())
	if err != nil {
		return fmt.Errorf("init JWT: %w", err)
	}

	db, err := database.OpenMySQL(cfg.MySQL)
	if err != nil {
		return err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	defer sqlDB.Close()

	client := redis.NewClient(&redis.Options{
		Addr: cfg.Redis.Addr, Username: cfg.Redis.Username, Password: cfg.Redis.Password, DB: cfg.Redis.DB,
	})
	defer client.Close()
	if err := client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("connect to Redis: %w", err)
	}
	if err := database.CreateMissingTables(ctx, db, &user.User{}, &community.Community{}, &post.Post{}, &post.Comment{}); err != nil {
		return err
	}
	if err := community.Seed(ctx, db); err != nil {
		return fmt.Errorf("seed communities: %w", err)
	}
	ids, err := snowflake.New(cfg.Snowflake.NodeID)
	if err != nil {
		return fmt.Errorf("init snowflake: %w", err)
	}
	sessions, err := session.New(tokens, client, cfg.JWT.SessionOptions())
	if err != nil {
		return fmt.Errorf("init sessions: %w", err)
	}
	userService := user.NewService(user.NewRepository(db), ids, sessions)
	if err := userService.SeedAdmin(ctx); err != nil {
		return fmt.Errorf("seed administrator: %w", err)
	}
	users := user.NewHandler(userService)
	ranking := vote.NewStore(client)
	posts := post.New(db, ids, ranking)
	if err := posts.Reindex(ctx); err != nil {
		return fmt.Errorf("rebuild post indexes: %w", err)
	}
	server := &http.Server{
		Addr:              cfg.Server.Addr,
		Handler:           router.New(db, users, posts, vote.NewHandler(ranking, posts.Exists), sessions),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	return serve(ctx, server)
}

// serve 停机时先等待正在处理的请求结束，再让 run 关闭数据库连接池。
func serve(ctx context.Context, server *http.Server) error {
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return fmt.Errorf("listen HTTP: %w", err)
	}
	defer listener.Close()

	result := make(chan error, 1)
	go func() {
		result <- server.Serve(listener)
	}()
	slog.Info("server listening", "address", listener.Addr().String())

	select {
	case err := <-result:
		return err
	case <-ctx.Done():
	}

	shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownContext); err != nil {
		server.Close()
		<-result
		return fmt.Errorf("shutdown HTTP: %w", err)
	}

	if err := <-result; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
