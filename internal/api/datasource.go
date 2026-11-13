package api

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"collect/internal/collector"
	"collect/internal/model"

	"github.com/gin-gonic/gin"
)

func (s *Server) listDataSources(c *gin.Context) {
	var list []model.DataSource
	if err := s.DB.Order("id asc").Find(&list).Error; err != nil {
		fail(c, err)
		return
	}
	ok(c, list)
}

func (s *Server) saveDataSource(c *gin.Context) {
	var ds model.DataSource
	if err := c.ShouldBindJSON(&ds); err != nil {
		failMsg(c, "参数错误: "+err.Error())
		return
	}
	ds.Name = strings.TrimSpace(ds.Name)
	if ds.Name == "" {
		failMsg(c, "请填写数据源名称")
		return
	}
	switch ds.Type {
	case "mysql", "oracle", "sqlite", "postgres":
	default:
		failMsg(c, "数据库类型必须是 mysql / oracle / sqlite / postgres")
		return
	}
	if ds.ID == 0 {
		ds.CreatedAt = time.Now()
		ds.UpdatedAt = time.Now()
		if err := s.DB.Create(&ds).Error; err != nil {
			fail(c, err)
			return
		}
	} else {
		var old model.DataSource
		if err := s.DB.First(&old, ds.ID).Error; err != nil {
			failMsg(c, "数据源不存在")
			return
		}
		if ds.Password == "" {
			ds.Password = old.Password
		}
		ds.CreatedAt = old.CreatedAt
		ds.UpdatedAt = time.Now()
		if err := s.DB.Save(&ds).Error; err != nil {
			fail(c, err)
			return
		}
	}
	ok(c, ds)
}

func (s *Server) deleteDataSource(c *gin.Context) {
	id, okID := idParam(c)
	if !okID {
		return
	}
	var count int64
	s.DB.Model(&model.Task{}).Where("sink LIKE ?", fmt.Sprintf("%%\"data_source_id\":%d%%", id)).Count(&count)
	if count > 0 {
		failMsg(c, fmt.Sprintf("有 %d 个任务正在使用该数据源，请先修改任务配置", count))
		return
	}
	if err := s.DB.Delete(&model.DataSource{}, id).Error; err != nil {
		fail(c, err)
		return
	}
	okMsg(c, "已删除", nil)
}

func testSource(ds model.DataSource) error {
	if ds.Type == "" {
		return errors.New("请选择数据库类型")
	}
	return collector.CheckConn(ds, 10*time.Second)
}

func (s *Server) testDataSourceBody(c *gin.Context) {
	var ds model.DataSource
	if err := c.ShouldBindJSON(&ds); err != nil {
		failMsg(c, "参数错误: "+err.Error())
		return
	}
	if ds.ID > 0 {
		var old model.DataSource
		if err := s.DB.First(&old, ds.ID).Error; err == nil {
			if ds.Password == "" {
				ds.Password = old.Password
			}
			if ds.DSN == "" {
				ds.DSN = old.DSN
			}
		}
	}
	if err := testSource(ds); err != nil {
		failMsg(c, "连接失败: "+err.Error())
		return
	}
	okMsg(c, "连接成功", nil)
}

func (s *Server) testDataSource(c *gin.Context) {
	id, okID := idParam(c)
	if !okID {
		return
	}
	var ds model.DataSource
	if err := s.DB.First(&ds, id).Error; err != nil {
		failMsg(c, "数据源不存在")
		return
	}
	if err := testSource(ds); err != nil {
		failMsg(c, "连接失败: "+err.Error())
		return
	}
	okMsg(c, "连接成功", nil)
}

// execDataSource 在数据源上执行一条 SQL，用于调试 SQL 模板 / 查看目标表
func (s *Server) execDataSource(c *gin.Context) {
	id, okID := idParam(c)
	if !okID {
		return
	}
	var ds model.DataSource
	if err := s.DB.First(&ds, id).Error; err != nil {
		failMsg(c, "数据源不存在")
		return
	}
	var body struct {
		SQL   string `json:"sql"`
		Limit int    `json:"limit"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		failMsg(c, "参数错误: "+err.Error())
		return
	}
	res, err := collector.ExecSQL(ds, body.SQL, body.Limit)
	if err != nil {
		failMsg(c, "执行失败: "+err.Error())
		return
	}
	ok(c, res)
}
