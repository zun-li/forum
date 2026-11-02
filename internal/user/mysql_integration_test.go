package user_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"forum/internal/api"
	"forum/internal/testutil/mysqltest"
	"forum/internal/user"
	"forum/pkg/password"
	"forum/pkg/snowflake"
)

// 显式指定测试实例 DSN；只创建和删除本次随机命名的数据库。
func TestMySQLSignupIntegration(t *testing.T) {
	db := mysqltest.Open(t, &user.User{})
	repo := user.NewRepository(db)
	r := testRouter(t, repo)
	codes := make(chan int, 4)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes <- code(t, request(r, "POST", "/api/v1/signup", `{"username":" Race_User ","password":"long password 123","re_password":"long password 123"}`, "application/json"))
		}()
	}
	wg.Wait()
	close(codes)
	counts := map[int]int{}
	for code := range codes {
		counts[code]++
	}
	if counts[api.Success] != 1 || counts[api.UserExists] != 3 {
		t.Fatalf("concurrent statuses: %v", counts)
	}
	var saved user.User
	if err := db.Where("username = ?", "race_user").First(&saved).Error; err != nil {
		t.Fatal(err)
	}
	if saved.ID < 1<<53 {
		t.Fatal("ID is not a contemporary snowflake")
	}
	ok, err := password.Verify("long password 123", saved.PasswordHash)
	if err != nil || !ok {
		t.Fatal("stored hash invalid")
	}
	// 主键碰撞必须作为内部错误，不能误报用户名重复。
	collision := user.User{ID: saved.ID, Username: "different", PasswordHash: "hash"}
	if err := repo.Create(context.Background(), &collision); err == nil || err == user.ErrUsernameTaken {
		t.Fatal("primary-key collision misclassified")
	}
	out := request(r, "GET", "/api/v1/users", "", "")
	if code(t, out) != api.Success || !strings.Contains(out.Body.String(), "race_user") || strings.Contains(out.Body.String(), "password") {
		t.Fatal(out.Body.String())
	}
	// 完整链路使用真实数据库：注册后的凭证登录，再访问受保护接口。
	result := login(t, r, `{"username":"RACE_USER","password":"long password 123"}`)
	current := me(r, "Bearer "+result.Token)
	if code(t, current) != api.Success || !strings.Contains(current.Body.String(), result.UserID) {
		t.Fatalf("current user lookup failed: %d", current.Code)
	}
	if err := db.Delete(&user.User{}, saved.ID).Error; err != nil {
		t.Fatal(err)
	}
	if response := me(r, "Bearer "+result.Token); code(t, response) != api.InvalidToken {
		t.Fatal("deleted user must not be returned by /me")
	}
}

func TestMySQLAdminBootstrap(t *testing.T) {
	db := mysqltest.Open(t, &user.User{})
	ctx := t.Context()
	repo := user.NewRepository(db)
	ids, err := snowflake.New(1)
	if err != nil {
		t.Fatal(err)
	}
	users := user.NewService(repo, ids, nil)
	legacy := user.User{ID: ids.Next(), Username: "admin", PasswordHash: "legacy"}
	if err := repo.Create(ctx, &legacy); err != nil {
		t.Fatal(err)
	}
	if err := users.SeedAdmin(ctx); err == nil {
		t.Fatal("an existing ordinary account cannot become the default administrator")
	}
	saved, err := repo.FindByUsername(ctx, "admin")
	if err != nil || saved.Role != user.RoleUser || saved.PasswordHash != "legacy" {
		t.Fatal("bootstrap modified legacy credentials", err)
	}
	if err := db.Model(&legacy).Update("username", "legacy_admin").Error; err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			if err := users.SeedAdmin(ctx); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	saved, err = repo.FindByUsername(ctx, "admin")
	if err != nil || saved.Role != user.RoleAdmin || !saved.MustChangePassword {
		t.Fatal("missing first-login administrator", err)
	}
	if ok, err := password.Verify("666666", saved.PasswordHash); err != nil || !ok {
		t.Fatal("wrong bootstrap password", err)
	}
	newHash, err := password.Hash("123456")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SetInitialPassword(ctx, saved.ID, newHash); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetInitialPassword(ctx, saved.ID, "stale hash"); !errors.Is(err, user.ErrPasswordAlreadySet) {
		t.Fatal("a stale first-login request could replace the password", err)
	}
	if err := users.SeedAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	saved, err = repo.FindByUsername(ctx, "admin")
	if err != nil || saved.MustChangePassword || saved.PasswordHash != newHash {
		t.Fatal("restarting reset the password", err)
	}
}
