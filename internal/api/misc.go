package api

import (
	"time"

	"collect/internal/model"

	"github.com/gin-gonic/gin"
)

var startTime = time.Now()

func (s *Server) health(c *gin.Context) {
	ok(c, gin.H{
		"status":  "ok",
		"version": "1.0.0",
		"uptime":  time.Since(startTime).Round(time.Second).String(),
		"time":    time.Now().Format("2006-01-02 15:04:05"),
	})
}

func (s *Server) logs(c *gin.Context) {
	after := int64(intQuery(c, "after", 0))
	limit := intQuery(c, "limit", 500)
	level := c.Query("level")
	entries := s.Log.List(after, limit, level)
	ok(c, gin.H{"list": entries})
}

func (s *Server) listNotifications(c *gin.Context) {
	page, size := pageQuery(c)
	q := s.DB.Model(&model.Notification{})
	if v := c.Query("read"); v == "true" {
		q = q.Where("read = ?", true)
	} else if v == "false" {
		q = q.Where("read = ?", false)
	}
	if v := c.Query("level"); v != "" {
		q = q.Where("level = ?", v)
	}
	var total int64
	q.Count(&total)
	var list []model.Notification
	if err := q.Order("id desc").Offset((page - 1) * size).Limit(size).Find(&list).Error; err != nil {
		fail(c, err)
		return
	}
	var unread int64
	s.DB.Model(&model.Notification{}).Where("read = ?", false).Count(&unread)
	ok(c, gin.H{"total": total, "unread": unread, "page": page, "page_size": size, "list": list})
}

func (s *Server) readNotification(c *gin.Context) {
	id, okID := idParam(c)
	if !okID {
		return
	}
	if err := s.DB.Model(&model.Notification{}).Where("id = ?", id).Update("read", true).Error; err != nil {
		fail(c, err)
		return
	}
	okMsg(c, "已读", nil)
}

func (s *Server) readAllNotifications(c *gin.Context) {
	if err := s.DB.Model(&model.Notification{}).Where("read = ?", false).Update("read", true).Error; err != nil {
		fail(c, err)
		return
	}
	okMsg(c, "全部已读", nil)
}

func (s *Server) deleteNotification(c *gin.Context) {
	id, okID := idParam(c)
	if !okID {
		return
	}
	if err := s.DB.Delete(&model.Notification{}, id).Error; err != nil {
		fail(c, err)
		return
	}
	okMsg(c, "已删除", nil)
}

func (s *Server) clearNotifications(c *gin.Context) {
	if err := s.DB.Where("1 = 1").Delete(&model.Notification{}).Error; err != nil {
		fail(c, err)
		return
	}
	okMsg(c, "已清空消息", nil)
}
