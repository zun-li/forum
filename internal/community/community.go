package community

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"forum/internal/api"

	"github.com/gin-gonic/gin"
	"github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Community struct {
	ID           uint64    `gorm:"primaryKey;autoIncrement:false" json:"id"`
	Name         string    `gorm:"size:128;uniqueIndex;not null" json:"name"`
	Introduction string    `gorm:"size:256;not null" json:"introduction"`
	CreatedAt    time.Time `gorm:"not null" json:"create_time"`
}

func RegisterAdmin(admin *gin.RouterGroup, db *gorm.DB) {
	admin.POST("/communities", func(c *gin.Context) {
		var input struct {
			Name         string `json:"name"`
			Introduction string `json:"introduction"`
		}
		if !api.DecodeJSON(c, &input, 4096) {
			return
		}
		channel := Community{Name: strings.TrimSpace(input.Name), Introduction: strings.TrimSpace(input.Introduction)}
		if channel.Name == "" || utf8.RuneCountInString(channel.Name) > 128 ||
			channel.Introduction == "" || utf8.RuneCountInString(channel.Introduction) > 256 {
			api.Fail(c, api.InvalidParam, "频道名称须为 1–128 个字符，简介须为 1–256 个字符")
			return
		}
		err := db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
			// Existing channel IDs are small JSON numbers, assigned by the server.
			// Lock the first seeded row so concurrent creators allocate distinct IDs.
			var first Community
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").First(&first, 1).Error; err != nil {
				return err
			}
			if err := tx.Model(&Community{}).Select("MAX(id) + 1").Scan(&channel.ID).Error; err != nil {
				return err
			}
			return tx.Create(&channel).Error
		})
		var duplicate *mysql.MySQLError
		switch {
		case errors.As(err, &duplicate) && duplicate.Number == 1062:
			api.Fail(c, api.Conflict, "频道名称已存在")
		case err != nil:
			slog.Error("create community", "error", err)
			api.Fail(c, api.ServerBusy, "服务繁忙")
		default:
			api.Created(c, channel)
		}
	})
}

func Seed(ctx context.Context, db *gorm.DB) error {
	zone := time.FixedZone("CST", 8*60*60)
	communities := []Community{
		{1, "编程与软件工程", "讨论编程语言、系统设计，以及代码的测试和维护。", time.Date(2016, 11, 1, 8, 10, 10, 0, zone)},
		{2, "算法与数据结构", "分享解题思路，比较不同算法的时间和空间复杂度。", time.Date(2020, 1, 1, 8, 0, 0, 0, zone)},
		{3, "机器学习", "讨论数据准备、模型训练和评估，分享实验结果。", time.Date(2018, 8, 7, 8, 30, 0, 0, zone)},
		{4, "大模型应用", "交流提示词设计、知识库问答和智能体开发经验。", time.Date(2016, 1, 1, 8, 0, 0, 0, zone)},
	}
	return db.WithContext(ctx).Clauses(clause.OnConflict{
		DoUpdates: clause.AssignmentColumns([]string{"name", "introduction"}),
	}).Create(&communities).Error
}

func Register(group *gin.RouterGroup, db *gorm.DB) {
	group.GET("/communities", func(c *gin.Context) {
		var summaries []struct {
			ID   uint64 `json:"id"`
			Name string `json:"name"`
		}
		err := db.WithContext(c.Request.Context()).Model(&Community{}).Order("id").Find(&summaries).Error
		if err != nil {
			slog.Error("list communities", "error", err)
			api.Fail(c, api.ServerBusy, "服务繁忙")
			return
		}
		api.OK(c, summaries)
	})

	group.GET("/communities/:id", func(c *gin.Context) {
		id, err := strconv.ParseUint(c.Param("id"), 10, 64)
		if err != nil || id == 0 {
			api.Fail(c, api.InvalidParam, "社区 ID 无效")
			return
		}
		var result Community
		err = db.WithContext(c.Request.Context()).First(&result, "id = ?", id).Error
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			api.Fail(c, api.NotFound, "社区不存在")
		case err != nil:
			slog.Error("get community", "error", err)
			api.Fail(c, api.ServerBusy, "服务繁忙")
		default:
			api.OK(c, result)
		}
	})
}
