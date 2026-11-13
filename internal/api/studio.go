package api

import (
	"collect/internal/studio"

	"github.com/gin-gonic/gin"
)

func (s *Server) studioStart(c *gin.Context) {
	var req struct {
		URL  string `json:"url"`
		Mode string `json:"mode"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		failMsg(c, "参数错误: "+err.Error())
		return
	}
	sess, err := s.Studio.Start(req.URL, req.Mode)
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, sess.Info())
}

func (s *Server) studioList(c *gin.Context) {
	ok(c, s.Studio.List())
}

func (s *Server) studioCapture(c *gin.Context) {
	data, err := s.Studio.Capture(c.Param("id"))
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, data)
}

func (s *Server) studioRequest(c *gin.Context) {
	req, err := s.Studio.RequestDetail(c.Param("id"), c.Param("reqId"))
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, req)
}

func (s *Server) studioStop(c *gin.Context) {
	if err := s.Studio.Stop(c.Param("id")); err != nil {
		fail(c, err)
		return
	}
	okMsg(c, "会话已停止", nil)
}

func (s *Server) studioRemove(c *gin.Context) {
	s.Studio.Remove(c.Param("id"))
	okMsg(c, "已删除会话", nil)
}

func (s *Server) studioDraft(c *gin.Context) {
	var req studio.DraftRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		failMsg(c, "参数错误: "+err.Error())
		return
	}
	draft, err := s.Studio.BuildDraft(c.Param("id"), req)
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, draft)
}
