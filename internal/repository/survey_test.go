package repository

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"cc-053/internal/models"
	"cc-053/internal/testsupport"
)

var (
	testDB     *sql.DB
	countyRepo *CountyRepo
	regionRepo *RegionRepo
	pointRepo  *SurveyPointRepo
)

func TestMain(m *testing.M) {
	db, err := testsupport.OpenFresh("repository")
	if err != nil {
		fmt.Println("test db:", err)
		os.Exit(1)
	}
	testDB = db
	countyRepo = NewCountyRepo(db)
	regionRepo = NewRegionRepo(db)
	pointRepo = NewSurveyPointRepo(db, regionRepo, countyRepo)
	os.Exit(m.Run())
}

func reset(t *testing.T) {
	t.Helper()
	_, err := testDB.Exec(`
		TRUNCATE point_merges, point_records, point_assignments, survey_point_aliases,
		         survey_points, dialect_regions, counties RESTART IDENTITY CASCADE`)
	if err != nil {
		t.Fatalf("truncate: %v", err)
	}
}

func date(s string) time.Time {
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return d
}

// 造三级区：l1(大区) > l2(片) > l3a/l3b(两片叶子)
type fixture struct {
	countyID             int64
	l1, l2, leafA, leafB int64
}

func setupHierarchy(t *testing.T) fixture {
	t.Helper()
	cy := &models.County{Name: "望江县", Code: "C340827"}
	if err := countyRepo.Create(cy); err != nil {
		t.Fatalf("county: %v", err)
	}
	r1 := &models.DialectRegion{Name: "江淮官话", Code: "JH"}
	if err := regionRepo.Create(r1); err != nil {
		t.Fatalf("l1: %v", err)
	}
	r2 := &models.DialectRegion{Name: "洪巢片", Code: "HC", ParentID: &r1.ID}
	if err := regionRepo.Create(r2); err != nil {
		t.Fatalf("l2: %v", err)
	}
	ra := &models.DialectRegion{Name: "安庆小片", Code: "AQ", ParentID: &r2.ID}
	if err := regionRepo.Create(ra); err != nil {
		t.Fatalf("leafA: %v", err)
	}
	rb := &models.DialectRegion{Name: "巢湖小片", Code: "CH", ParentID: &r2.ID}
	if err := regionRepo.Create(rb); err != nil {
		t.Fatalf("leafB: %v", err)
	}
	if ra.IsLeaf != true || rb.IsLeaf != true {
		t.Fatalf("expected new regions to be leaves")
	}
	// 加上下级后，l2 不再是叶子（重新查库确认）
	r2ref, err := regionRepo.GetByID(r2.ID)
	if err != nil {
		t.Fatal(err)
	}
	if r2ref.IsLeaf {
		t.Fatalf("洪巢片有下级，不应是叶子")
	}
	return fixture{countyID: cy.ID, l1: r1.ID, l2: r2.ID, leafA: ra.ID, leafB: rb.ID}
}

func TestCreatePoint_DuplicateVariantNameRejected(t *testing.T) {
	reset(t)
	f := setupHierarchy(t)

	p1 := &models.SurveyPoint{Name: "长沙市", Code: "P001", CountyID: f.countyID}
	if err := pointRepo.Create(p1, []string{"长沙"}, nil, time.Time{}, ""); err != nil {
		t.Fatalf("create p1: %v", err)
	}

	// 同一个地方，繁体 + 多余空白与标点 -> 指纹相同，当场指出
	p2 := &models.SurveyPoint{Name: " 長沙，市 ", Code: "P002", CountyID: f.countyID}
	err := pointRepo.Create(p2, nil, nil, time.Time{}, "")
	var biz *BizError
	if !errors.As(err, &biz) || biz.Code != "conflict" {
		t.Fatalf("期望 conflict 业务错误，got %v", err)
	}
	t.Logf("重复建档指认：%s | %s", biz.Message, biz.Detail)
	if !strings.Contains(biz.Detail, "长沙市") {
		t.Fatalf("detail 应指认既有档案名，got %s", biz.Detail)
	}

	// 别名撞已有名也要拦
	p3 := &models.SurveyPoint{Name: "别的镇", Code: "P003", CountyID: f.countyID}
	err = pointRepo.Create(p3, []string{"长沙"}, nil, time.Time{}, "")
	if !errors.As(err, &biz) || biz.Code != "conflict" {
		t.Fatalf("别名撞名应 conflict，got %v", err)
	}

	// code 重复
	p4 := &models.SurveyPoint{Name: "另一个点", Code: "P001", CountyID: f.countyID}
	if err = pointRepo.Create(p4, nil, nil, time.Time{}, ""); !isBiz(err) {
		t.Fatalf("重复 code 应报业务错误，got %v", err)
	}
}

func TestAssign_MustBeLeaf(t *testing.T) {
	reset(t)
	f := setupHierarchy(t)
	p := &models.SurveyPoint{Name: "测试点", Code: "Q001", CountyID: f.countyID}
	if err := pointRepo.Create(p, nil, nil, time.Time{}, ""); err != nil {
		t.Fatal(err)
	}
	// 挂到“片”（还有下级）必须被拒
	_, err := pointRepo.AdjustAssignment(p.ID, f.l2, date("2020-01-01"), "admin", "")
	var biz *BizError
	if !errors.As(err, &biz) || biz.Code != "not_leaf" {
		t.Fatalf("挂非叶子区应 not_leaf，got %v", err)
	}
}

func TestAssignmentTimeline_AdjustAndAsOf(t *testing.T) {
	reset(t)
	f := setupHierarchy(t)
	p := &models.SurveyPoint{Name: "码头镇", Code: "T001", CountyID: f.countyID}
	// 建档即挂 leafA，自 2020-01-01 起
	if err := pointRepo.Create(p, nil, &f.leafA, date("2020-01-01"), "admin"); err != nil {
		t.Fatal(err)
	}

	// 调整到 leafB，自 2023-06-01 起
	if _, err := pointRepo.AdjustAssignment(p.ID, f.leafB, date("2023-06-01"), "admin", "重新分区"); err != nil {
		t.Fatalf("调整归属: %v", err)
	}

	// 同日再调一次 -> 挂重，当场拒绝
	_, err := pointRepo.AdjustAssignment(p.ID, f.leafA, date("2023-06-01"), "admin", "")
	var biz *BizError
	if !errors.As(err, &biz) || biz.Code != "overlap" {
		t.Fatalf("同日重复挂接应 overlap，got %v", err)
	}
	// 调到过去（早于现行起点）也拒绝
	if _, err = pointRepo.AdjustAssignment(p.ID, f.leafA, date("2022-01-01"), "admin", ""); !isBiz(err) {
		t.Fatalf("回溯日期造成重叠应拒绝，got %v", err)
	}

	// 按当时归属回看
	at2021, err := pointRepo.AssignmentAt(p.ID, date("2021-05-01"))
	if err != nil {
		t.Fatal(err)
	}
	if at2021.RegionID != f.leafA || at2021.RegionName != "安庆小片" {
		t.Fatalf("2021 应属安庆小片，got %+v", at2021)
	}
	if at2021.RegionPath != "江淮官话/洪巢片/安庆小片" {
		t.Fatalf("区路径不符: %q", at2021.RegionPath)
	}
	at2024, err := pointRepo.AssignmentAt(p.ID, date("2024-01-01"))
	if err != nil {
		t.Fatal(err)
	}
	if at2024.RegionID != f.leafB || at2024.RegionName != "巢湖小片" {
		t.Fatalf("2024 应属巢湖小片，got %+v", at2024)
	}
	// 生效日当天即算新区（左闭）
	atBound, _ := pointRepo.AssignmentAt(p.ID, date("2023-06-01"))
	if atBound.RegionID != f.leafB {
		t.Fatalf("2023-06-01 当天应属新归属")
	}
	// 早于任何归属 -> 无
	if a, err := pointRepo.AssignmentAt(p.ID, date("2019-01-01")); err != nil || a != nil {
		t.Fatalf("2019 应无归属，got %+v err=%v", a, err)
	}

	// 时间线两段
	asg, err := pointRepo.Assignments(p.ID)
	if err != nil || len(asg) != 2 {
		t.Fatalf("应有 2 段归属，got %d err=%v", len(asg), err)
	}
	if asg[0].EndDate != nil || asg[1].EndDate == nil {
		t.Fatalf("倒序首段应为现行(NULL end_date)，末段为历史段(有 end_date)")
	}
}

func TestRecordSnapshot_FrozenAfterAdjust(t *testing.T) {
	reset(t)
	f := setupHierarchy(t)
	p := &models.SurveyPoint{Name: "码头镇", Code: "T100", CountyID: f.countyID}
	if err := pointRepo.Create(p, nil, &f.leafA, date("2020-01-01"), "admin"); err != nil {
		t.Fatal(err)
	}

	// 2021 年发出一条说法，当时属 leafA
	rec, err := pointRepo.CreateRecord(p.ID, "「吃」读 tɕʰiəʔ", date("2021-03-01"), "zhang")
	if err != nil {
		t.Fatal(err)
	}
	if rec.SnapshotRegion != "安庆小片" || rec.SnapshotCounty != "望江县" ||
		rec.SnapshotPath != "江淮官话/洪巢片/安庆小片" {
		t.Fatalf("快照不符: %+v", rec)
	}

	// 之后调到 leafB
	if _, err := pointRepo.AdjustAssignment(p.ID, f.leafB, date("2023-06-01"), "admin", ""); err != nil {
		t.Fatal(err)
	}
	// 回看旧说法：快照不许跟着变
	recs, err := pointRepo.Records(p.ID)
	if err != nil || len(recs) != 1 {
		t.Fatalf("records: %v %v", recs, err)
	}
	if recs[0].SnapshotRegion != "安庆小片" {
		t.Fatalf("已发说法的归属快照被改动：%s", recs[0].SnapshotRegion)
	}
	// 新说法按新归属固化
	rec2, err := pointRepo.CreateRecord(p.ID, "「喝」读 xɔ", date("2024-01-01"), "li")
	if err != nil {
		t.Fatal(err)
	}
	if rec2.SnapshotRegion != "巢湖小片" {
		t.Fatalf("新说法应按新归属快照，got %s", rec2.SnapshotRegion)
	}

	// 没有归属的日期登记说法 -> 拒绝
	if _, err = pointRepo.CreateRecord(p.ID, "太早了", date("2019-01-01"), ""); err == nil {
		t.Fatalf("无归属日期应拒绝登记")
	} else {
		var biz *BizError
		if !errors.As(err, &biz) || biz.Code != "no_assignment" {
			t.Fatalf("应 no_assignment，got %v", err)
		}
	}

	// 触发器：直接改/删快照必须失败
	if _, err = testDB.Exec(`UPDATE point_records SET snapshot_region='被篡改' WHERE id=$1`, rec.ID); err == nil {
		t.Fatal("更新快照竟成功，触发器未生效")
	}
	if _, err = testDB.Exec(`DELETE FROM point_records WHERE id=$1`, rec.ID); err == nil {
		t.Fatal("删除快照竟成功，触发器未生效")
	}
}

func TestMergePoints_CountsReconcile(t *testing.T) {
	reset(t)
	f := setupHierarchy(t)

	// A：规范名「长沙市」，2 条
	a := &models.SurveyPoint{Name: "长沙市", Code: "M001", CountyID: f.countyID}
	if err := pointRepo.Create(a, nil, &f.leafA, date("2020-01-01"), ""); err != nil {
		t.Fatal(err)
	}
	// B：同一地方的另一个完全不同的名字「汩罗镇」。
	// 指纹不同、正常建档拦不住（判定是否同一处靠档案员），正是走合并流程的场景。
	b := &models.SurveyPoint{Name: "汩罗镇", Code: "M002", CountyID: f.countyID}
	if err := pointRepo.Create(b, []string{"汩罗"}, &f.leafA, date("2020-01-01"), ""); err != nil {
		t.Fatal(err)
	}
	for i, txt := range []string{"a1", "a2"} {
		if _, err := pointRepo.CreateRecord(a.ID, txt, date("2021-01-01").AddDate(0, i, 0), ""); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pointRepo.CreateRecord(b.ID, "b1", date("2021-05-01"), ""); err != nil {
		t.Fatal(err)
	}

	// 不能自己并自己
	if _, err := pointRepo.Merge(a.ID, a.ID, "admin", ""); err == nil {
		t.Fatal("自合并应拒绝")
	}

	m, err := pointRepo.Merge(a.ID, b.ID, "admin", "确认同一地点")
	if err != nil {
		t.Fatalf("合并: %v", err)
	}
	if m.SurvivingBefore != 2 || m.MergedBefore != 1 || m.TotalAfter != 3 {
		t.Fatalf("条数对不上: %+v", m)
	}

	// 合并后：A 有 3 条，B 有 0 条并标记 merged
	if n, _ := pointRepo.countRecordsByID(a.ID); n != 3 {
		t.Fatalf("A 合并后应 3 条，got %d", n)
	}
	if n, _ := pointRepo.countRecordsByID(b.ID); n != 0 {
		t.Fatalf("B 合并后应 0 条，got %d", n)
	}
	bg, err := pointRepo.GetByID(b.ID)
	if err != nil || bg.Status != "merged" || bg.MergedIntoID == nil || *bg.MergedIntoID != a.ID {
		t.Fatalf("B 应标记 merged 指向 A: %+v err=%v", bg, err)
	}

	// 别名归并：A 现在也能认出 B 的名字
	ag, err := pointRepo.GetByID(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(ag.Aliases, "汩罗镇") || !contains(ag.Aliases, "汩罗") {
		t.Fatalf("别名未归并到 A: %v", ag.Aliases)
	}

	// 再用「长沙市」建档，仍指向存活的 A
	dup := &models.SurveyPoint{Name: "长沙市", Code: "M009", CountyID: f.countyID}
	var biz *BizError
	if err := pointRepo.Create(dup, nil, nil, time.Time{}, ""); !errors.As(err, &biz) || biz.Code != "conflict" {
		t.Fatalf("合并后旧名仍应被认出，got %v", err)
	}

	// 已合并的点不能再调整归属/登记/再合并
	if _, err := pointRepo.AdjustAssignment(b.ID, f.leafB, date("2024-01-01"), "", ""); !isBiz(err) {
		t.Fatalf("已合并点调整归属应拒绝，got %v", err)
	}
	if _, err := pointRepo.Merge(b.ID, a.ID, "", ""); !isBiz(err) {
		t.Fatalf("已合并点再合并应拒绝，got %v", err)
	}
}

func (r *SurveyPointRepo) countRecordsByID(id int64) (int, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	n, err := r.countRecordsTx(tx, id)
	return n, err
}

func isBiz(err error) bool {
	var biz *BizError
	return errors.As(err, &biz)
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

func TestListFilters(t *testing.T) {
	reset(t)
	f := setupHierarchy(t)
	// 点1：county=f.countyID, leafA 自2020
	p1 := &models.SurveyPoint{Name: "码头镇", Code: "L001", CountyID: f.countyID}
	if err := pointRepo.Create(p1, nil, &f.leafA, date("2020-01-01"), ""); err != nil {
		t.Fatal(err)
	}
	// 点2：leafB
	p2 := &models.SurveyPoint{Name: "漳湖圩", Code: "L002", CountyID: f.countyID}
	if err := pointRepo.Create(p2, nil, &f.leafB, date("2020-01-01"), ""); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		f    SurveyPointFilter
		want int
	}{
		{"按县", SurveyPointFilter{CountyID: f.countyID, Limit: 20}, 2},
		{"按叶子区A", SurveyPointFilter{RegionID: f.leafA, Limit: 20}, 1},
		{"按叶子区B", SurveyPointFilter{RegionID: f.leafB, Limit: 20}, 1},
		{"按上级片(含两个子区)", SurveyPointFilter{RegionID: f.l2, Limit: 20}, 2},
		{"按一级大区(全含)", SurveyPointFilter{RegionID: f.l1, Limit: 20}, 2},
		{"繁体关键字归一", SurveyPointFilter{Keyword: "碼頭", Limit: 20}, 1},
		{"关键字无结果", SurveyPointFilter{Keyword: "不存在", Limit: 20}, 0},
	}
	for _, tc := range tests {
		items, total, err := pointRepo.List(tc.f)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if total != tc.want || len(items) != tc.want {
			t.Fatalf("%s: total=%d len=%d want %d", tc.name, total, len(items), tc.want)
		}
	}

	// p1 调到 leafB 后，leafA 过滤为 0、leafB 为 2
	if _, err := pointRepo.AdjustAssignment(p1.ID, f.leafB, date("2023-01-01"), "", ""); err != nil {
		t.Fatal(err)
	}
	if items, total, _ := pointRepo.List(SurveyPointFilter{RegionID: f.leafA, Limit: 20}); total != 0 || len(items) != 0 {
		t.Fatalf("调整后 leafA 应无点，got total=%d", total)
	}
	if _, total, _ := pointRepo.List(SurveyPointFilter{RegionID: f.leafB, Limit: 20}); total != 2 {
		t.Fatalf("调整后 leafB 应有 2 点，got %d", total)
	}
}

func TestNormalizeName(t *testing.T) {
	cases := map[string]string{
		"长沙市":    "长沙市",
		"長沙市":    "长沙市",
		" 長沙 市 ": "长沙市",
		"长沙，市！":  "长沙市",
	}
	var base string
	for in, want := range cases {
		got := NormalizeName(in)
		if base == "" {
			base = got
		}
		if got != want {
			t.Fatalf("NormalizeName(%q)=%q want %q", in, got, want)
		}
	}
}
