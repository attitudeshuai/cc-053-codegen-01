package handlers

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"

	"cc-053/internal/repository"
	"cc-053/internal/testsupport"
)

var (
	tdb     *sql.DB
	router  *gin.Engine
	countyH *CountyHandler
	regionH *RegionHandler
	pointH  *SurveyPointHandler
)

func TestMain(m *testing.M) {
	db, err := testsupport.OpenFresh("handlers")
	if err != nil {
		panic(err)
	}
	tdb = db

	countyRepo := repository.NewCountyRepo(db)
	regionRepo := repository.NewRegionRepo(db)
	pointRepo := repository.NewSurveyPointRepo(db, regionRepo, countyRepo)
	countyH = NewCountyHandler(countyRepo)
	regionH = NewRegionHandler(regionRepo)
	pointH = NewSurveyPointHandler(pointRepo)

	gin.SetMode(gin.TestMode)
	router = gin.New()
	v1 := router.Group("/api/v1")
	v1.POST("/counties", countyH.Create)
	v1.GET("/counties/:id", countyH.GetByID)
	v1.POST("/dialect-regions", regionH.Create)
	v1.GET("/dialect-regions/tree", regionH.Tree)
	v1.POST("/survey-points", pointH.Create)
	v1.GET("/survey-points", pointH.List)
	v1.GET("/survey-points/:id", pointH.GetByID)
	v1.PUT("/survey-points/:id/assignment", pointH.Adjust)
	v1.GET("/survey-points/:id/assignments", pointH.Assignments)
	v1.GET("/survey-points/:id/assignment-at", pointH.AssignmentAt)
	v1.POST("/survey-points/:id/records", pointH.CreateRecord)
	v1.GET("/survey-points/:id/history", pointH.History)
	v1.POST("/survey-points/merge", pointH.Merge)

	os.Exit(m.Run())
}

func resetH(t *testing.T) {
	t.Helper()
	_, err := tdb.Exec(`
		TRUNCATE point_merges, point_records, point_assignments, survey_point_aliases,
		         survey_points, dialect_regions, counties RESTART IDENTITY CASCADE`)
	if err != nil {
		t.Fatal(err)
	}
}

func doJSON(t *testing.T, method, path string, body any) (int, map[string]any) {
	t.Helper()
	var rdr *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, rdr)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	var out map[string]any
	if w.Body.Len() > 0 {
		_ = json.Unmarshal(w.Body.Bytes(), &out)
	}
	return w.Code, out
}

func dataID(t *testing.T, resp map[string]any) int64 {
	t.Helper()
	data, ok := resp["data"].(map[string]any)
	if !ok {
		t.Fatalf("响应缺 data: %v", resp)
	}
	return int64(data["id"].(float64))
}

// 完整端到端：县 → 三级区 → 建档 → 查重 → 调整 → 回看 → 说法快照 → 合并
func TestSurveyArchiveEndToEnd(t *testing.T) {
	resetH(t)

	// 县
	st, resp := doJSON(t, "POST", "/api/v1/counties", map[string]any{"name": "望江县", "code": "WJ"})
	if st != http.StatusCreated {
		t.Fatalf("建县: %d %v", st, resp)
	}
	countyID := dataID(t, resp)

	// 一级区
	st, resp = doJSON(t, "POST", "/api/v1/dialect-regions", map[string]any{"name": "江淮官话", "code": "JH"})
	if st != 201 {
		t.Fatalf("建一级区: %d %v", st, resp)
	}
	l1 := dataID(t, resp)
	// 片
	st, resp = doJSON(t, "POST", "/api/v1/dialect-regions", map[string]any{"name": "洪巢片", "code": "HC", "parent_id": l1})
	if st != 201 {
		t.Fatalf("建片区: %d %v", st, resp)
	}
	l2 := dataID(t, resp)
	// 两个叶子小片
	mkLeaf := func(code, name string, parent int64) int64 {
		st, r := doJSON(t, "POST", "/api/v1/dialect-regions", map[string]any{"name": name, "code": code, "parent_id": parent})
		if st != 201 {
			t.Fatalf("建叶子区 %s: %d %v", name, st, r)
		}
		return dataID(t, r)
	}
	leafA := mkLeaf("AQ", "安庆小片", l2)
	leafB := mkLeaf("CH", "巢湖小片", l2)

	// 树可正常返回且一级区下挂着片区、片区下挂着两个叶子
	st, tree := doJSON(t, "GET", "/api/v1/dialect-regions/tree", nil)
	if st != 200 {
		t.Fatalf("树: %d", st)
	}
	roots := tree["data"].([]any)
	if len(roots) == 0 {
		t.Fatalf("方言区树为空")
	}
	root := roots[0].(map[string]any)
	kids := root["children"].([]any)
	if len(kids) != 1 || len(kids[0].(map[string]any)["children"].([]any)) != 2 {
		t.Fatalf("树结构不符：%v", root)
	}

	// 建档并直接挂 leafA
	st, resp = doJSON(t, "POST", "/api/v1/survey-points", map[string]any{
		"name": "码头镇", "code": "D001", "county_id": countyID,
		"region_id": leafA, "effective_date": "2020-01-01", "created_by": "admin",
	})
	if st != 201 {
		t.Fatalf("建档: %d %v", st, resp)
	}
	p1 := dataID(t, resp)

	// 同一地方另一种写法（简体码頭→码头，去空白）→ 409 当场指出
	st, resp = doJSON(t, "POST", "/api/v1/survey-points", map[string]any{
		"name": " 碼頭鎮 ", "code": "D002", "county_id": countyID,
	})
	if st != http.StatusConflict {
		t.Fatalf("重复建档应 409，got %d %v", st, resp)
	}
	if detail, _ := resp["detail"].(string); !bytes.Contains([]byte(detail), []byte("码头镇")) {
		t.Fatalf("409 detail 应指认既有档案：%v", resp)
	}

	// 挂到非叶子"片" → 409
	st, _ = doJSON(t, "PUT", "/api/v1/survey-points/"+itoa(p1)+"/assignment",
		map[string]any{"region_id": l2, "effective_date": "2021-01-01"})
	if st != http.StatusConflict {
		t.Fatalf("挂非叶子应 409，got %d", st)
	}

	// 缺生效日 → 400
	st, _ = doJSON(t, "PUT", "/api/v1/survey-points/"+itoa(p1)+"/assignment",
		map[string]any{"region_id": leafB})
	if st != http.StatusBadRequest {
		t.Fatalf("缺生效日应 400，got %d", st)
	}

	// 调整归属到 leafB，2023-06-01 起
	st, resp = doJSON(t, "PUT", "/api/v1/survey-points/"+itoa(p1)+"/assignment",
		map[string]any{"region_id": leafB, "effective_date": "2023-06-01", "note": "重新分区"})
	if st != 200 {
		t.Fatalf("调整归属: %d %v", st, resp)
	}
	// 同日重复挂 → 409
	st, _ = doJSON(t, "PUT", "/api/v1/survey-points/"+itoa(p1)+"/assignment",
		map[string]any{"region_id": leafA, "effective_date": "2023-06-01"})
	if st != http.StatusConflict {
		t.Fatalf("同日挂重应 409，got %d", st)
	}

	// 按当时归属回看
	st, resp = doJSON(t, "GET", "/api/v1/survey-points/"+itoa(p1)+"/assignment-at?date=2021-01-01", nil)
	if got := resp["data"].(map[string]any)["region_id"].(float64); int64(got) != leafA {
		t.Fatalf("2021 应属 leafA，got %v", resp)
	}
	st, resp = doJSON(t, "GET", "/api/v1/survey-points/"+itoa(p1)+"/assignment-at?date=2024-01-01", nil)
	if got := resp["data"].(map[string]any)["region_id"].(float64); int64(got) != leafB {
		t.Fatalf("2024 应属 leafB，got %v", resp)
	}

	// 旧说法（2021）按旧归属、新说法（2024）按新归属
	st, r1 := doJSON(t, "POST", "/api/v1/survey-points/"+itoa(p1)+"/records",
		map[string]any{"content": "旧说法", "record_date": "2021-03-01"})
	if st != 201 || r1["data"].(map[string]any)["snapshot_region"] != "安庆小片" {
		t.Fatalf("旧说法快照: %d %v", st, r1)
	}
	st, r2 := doJSON(t, "POST", "/api/v1/survey-points/"+itoa(p1)+"/records",
		map[string]any{"content": "新说法", "record_date": "2024-03-01"})
	if st != 201 || r2["data"].(map[string]any)["snapshot_region"] != "巢湖小片" {
		t.Fatalf("新说法快照: %d %v", st, r2)
	}
	// 无归属的日期 → 409
	st, _ = doJSON(t, "POST", "/api/v1/survey-points/"+itoa(p1)+"/records",
		map[string]any{"content": "太早", "record_date": "2019-03-01"})
	if st != http.StatusConflict {
		t.Fatalf("无归属日期登记应 409，got %d", st)
	}

	// 回看历史：旧说法快照不随后续调整改变
	st, hist := doJSON(t, "GET", "/api/v1/survey-points/"+itoa(p1)+"/history", nil)
	if st != 200 {
		t.Fatalf("history: %d", st)
	}
	recs := hist["data"].(map[string]any)["records"].([]any)
	foundOld := false
	for _, rc := range recs {
		r := rc.(map[string]any)
		if r["content"] == "旧说法" && r["snapshot_region"] != "安庆小片" {
			t.Fatalf("已发说法快照被改变：%v", r)
		}
		if r["content"] == "旧说法" {
			foundOld = true
		}
	}
	if !foundOld {
		t.Fatal("历史里找不到旧说法")
	}

	// 建第二个点（不同名字）+1 条说法，合并到 p1
	st, resp = doJSON(t, "POST", "/api/v1/survey-points", map[string]any{
		"name": "漳湖圩", "code": "D010", "county_id": countyID,
		"region_id": leafB, "effective_date": "2022-01-01",
	})
	if st != 201 {
		t.Fatalf("建第二点: %d %v", st, resp)
	}
	p2 := dataID(t, resp)
	if st, _ = doJSON(t, "POST", "/api/v1/survey-points/"+itoa(p2)+"/records",
		map[string]any{"content": "b点说法", "record_date": "2022-05-01"}); st != 201 {
		t.Fatalf("b点说法: %d", st)
	}
	st, resp = doJSON(t, "POST", "/api/v1/survey-points/merge",
		map[string]any{"surviving_id": p1, "merged_id": p2, "merged_by": "admin", "note": "同一地点"})
	if st != 200 {
		t.Fatalf("合并: %d %v", st, resp)
	}
	mdata := resp["data"].(map[string]any)
	if mdata["surviving_before"].(float64) != 2 || mdata["merged_before"].(float64) != 1 ||
		mdata["total_after"].(float64) != 3 {
		t.Fatalf("合并条数对不上: %v", mdata)
	}
	// 合并后 p1 共 3 条
	st, hist = doJSON(t, "GET", "/api/v1/survey-points/"+itoa(p1)+"/history", nil)
	if len(hist["data"].(map[string]any)["records"].([]any)) != 3 {
		t.Fatalf("合并后 p1 应有 3 条说法")
	}
	// p2 标记 merged
	st, resp = doJSON(t, "GET", "/api/v1/survey-points/"+itoa(p2), nil)
	if resp["data"].(map[string]any)["status"] != "merged" {
		t.Fatalf("p2 应为 merged: %v", resp)
	}
}

func itoa(n int64) string {
	return strconv.FormatInt(n, 10)
}
