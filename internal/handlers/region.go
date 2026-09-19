package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"cc-053/internal/models"
	"cc-053/internal/repository"
)

type RegionHandler struct {
	repo *repository.RegionRepo
}

func NewRegionHandler(repo *repository.RegionRepo) *RegionHandler {
	return &RegionHandler{repo: repo}
}

func (h *RegionHandler) Create(c *gin.Context) {
	var req models.CreateRegionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Code: 400, Message: "invalid request", Detail: err.Error()})
		return
	}
	g := &models.DialectRegion{Name: req.Name, Code: req.Code, ParentID: req.ParentID, Note: req.Note}
	if err := h.repo.Create(g); err != nil {
		respondBiz(c, err, "failed to create dialect region")
		return
	}
	c.JSON(http.StatusCreated, models.APIResponse{Code: 201, Message: "dialect region created", Data: g})
}

func (h *RegionHandler) GetByID(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Code: 400, Message: "invalid id"})
		return
	}
	g, err := h.repo.GetByID(id)
	if err != nil {
		respondBiz(c, err, "failed to get dialect region")
		return
	}
	c.JSON(http.StatusOK, models.APIResponse{Code: 200, Data: g})
}

func (h *RegionHandler) List(c *gin.Context) {
	var p models.Pagination
	_ = c.ShouldBindQuery(&p)
	p.Normalize()
	items, total, err := h.repo.List(p.Offset, p.Limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Code: 500, Message: "failed to list regions"})
		return
	}
	c.JSON(http.StatusOK, models.APIResponse{Code: 200, Data: gin.H{"items": items, "total": total, "offset": p.Offset, "limit": p.Limit}})
}

// regionNode 树形返回
type regionNode struct {
	*models.DialectRegion
	Children []*regionNode `json:"children"`
}

// Tree 返回整棵方言区树，is_leaf=true 的节点才能挂调查点
func (h *RegionHandler) Tree(c *gin.Context) {
	all, err := h.repo.All()
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.ErrorResponse{Code: 500, Message: "failed to load region tree"})
		return
	}
	nodes := map[int64]*regionNode{}
	roots := []*regionNode{}
	// 先建节点
	for _, g := range all {
		nodes[g.ID] = &regionNode{DialectRegion: g, Children: []*regionNode{}}
	}
	// 再挂父子
	for _, g := range all {
		node := nodes[g.ID]
		if g.ParentID == nil {
			roots = append(roots, node)
		} else if parent, ok := nodes[*g.ParentID]; ok {
			parent.Children = append(parent.Children, node)
		} else {
			roots = append(roots, node) // 容错：父缺失时当根
		}
	}
	c.JSON(http.StatusOK, models.APIResponse{Code: 200, Data: roots})
}
