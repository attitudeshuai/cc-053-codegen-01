package repository

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/lib/pq"

	"cc-053/internal/database"
	"cc-053/internal/models"
)

func testDB(t *testing.T) *sql.DB {
	// 可用 TEST_DATABASE_DSN 覆盖；默认对应本机免安装测试实例（见 tests/ 下说明）
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		dsn = "host=/tmp/pgrun user=postgres dbname=dialect_archive_test sslmode=disable"
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Ping(); err != nil {
		t.Skipf("local postgres not available: %v", err)
	}
	if err := database.RunMigrations(db); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	return db
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func TestArchiveLifecycle(t *testing.T) {
	if os.Getenv("RUN_PG_TEST") == "" {
		t.Skip("set RUN_PG_TEST=1")
	}
	db := testDB(t)
	defer db.Close()
	countyRepo := NewCountyRepo(db)
	regionRepo := NewRegionRepo(db)
	pointRepo := NewSurveyPointRepo(db)
	suffix := fmt.Sprintf("-%d", time.Now().UnixNano())

	// ---- 县 ----
	county := &models.County{Name: "余庆县", Code: "YUQ" + suffix}
	if err := countyRepo.Create(county); err != nil {
		t.Fatalf("create county: %v", err)
	}
	county2 := &models.County{Name: "湄潭县", Code: "MT" + suffix}
	if err := countyRepo.Create(county2); err != nil {
		t.Fatal(err)
	}
	if _, err := countyRepo.GetByID(county.ID); err != nil {
		t.Fatal(err)
	}

	// ---- 分区层级 ----
	l1 := &models.DialectRegion{Name: "西南官话", Level: 1}
	if err := regionRepo.Create(l1); err != nil {
		t.Fatal(err)
	}
	l2 := &models.DialectRegion{Name: "川黔片", Level: 2, ParentID: &l1.ID}
	if err := regionRepo.Create(l2); err != nil {
		t.Fatal(err)
	}
	leafA := &models.DialectRegion{Name: "黔中小片", Level: 3, ParentID: &l2.ID}
	if err := regionRepo.Create(leafA); err != nil {
		t.Fatal(err)
	}
	leafB := &models.DialectRegion{Name: "黔南小片", Level: 3, ParentID: &l2.ID}
	if err := regionRepo.Create(leafB); err != nil {
		t.Fatal(err)
	}
	// 层级断档
	bad := &models.DialectRegion{Name: "断档", Level: 3, ParentID: &l1.ID}
	if err := regionRepo.Create(bad); !errors.Is(err, ErrLevelGap) {
		t.Fatalf("want ErrLevelGap, got %v", err)
	}
	// 顶级不是 level 1
	bad = &models.DialectRegion{Name: "伪大区", Level: 2}
	if err := regionRepo.Create(bad); !errors.Is(err, ErrLevelGap) {
		t.Fatalf("want ErrLevelGap, got %v", err)
	}
	if leaf, _ := regionRepo.IsLeaf(l2.ID); leaf {
		t.Fatal("l2 should not be leaf")
	}
	if leaf, _ := regionRepo.IsLeaf(leafA.ID); !leaf {
		t.Fatal("leafA should be leaf")
	}
	path, err := regionRepo.PathOf(leafA.ID)
	if err != nil || len(path) != 3 || path[0].ID != l1.ID {
		t.Fatalf("path wrong: %v err=%v", path, err)
	}

	// ---- 调查点 ----
	// 挂非叶子必须拒绝
	pBad := &models.SurveyPoint{Name: "x", Code: "X" + suffix, CountyID: county.ID}
	if err := pointRepo.Create(pBad, l1.ID); !errors.Is(err, ErrRegionNotLeaf) {
		t.Fatalf("want ErrRegionNotLeaf, got %v", err)
	}

	pA := &models.SurveyPoint{
		Name: "白泥镇" + suffix, Code: "BN" + suffix, CountyID: county.ID,
		Aliases: []string{"白泥" + suffix},
	}
	if err := pointRepo.Create(pA, leafA.ID); err != nil {
		t.Fatalf("create point A: %v", err)
	}

	// 认重：同县同名 / 同别名
	if dup, _ := pointRepo.FindConflict(pA.Name, county.ID); dup == nil || dup.ID != pA.ID {
		t.Fatal("same-county same-name should conflict")
	}
	if dup, _ := pointRepo.FindConflict(pA.Aliases[0], county2.ID); dup == nil {
		t.Fatal("alias match should conflict across counties")
	}
	if dup, _ := pointRepo.FindConflict(pA.Name, county2.ID); dup != nil {
		t.Fatal("same name in different county must NOT conflict")
	}

	// 跨县同名点可以建
	pOther := &models.SurveyPoint{Name: pA.Name, Code: "BN2" + suffix, CountyID: county2.ID}
	if err := pointRepo.Create(pOther, 0); err != nil {
		t.Fatalf("cross-county same-name: %v", err)
	}

	// ---- 归属时间线 ----
	d2026 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	dBefore := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	dAfter := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	// 同区重复挂 → 409 类错误
	if _, err := pointRepo.AssignRegion(pA.ID, leafA.ID, d2026, "li", ""); !errors.Is(err, ErrDoubleAssignment) {
		t.Fatalf("same-region reassign want ErrDoubleAssignment, got %v", err)
	}
	// 正常调整到 B
	if _, err := pointRepo.AssignRegion(pA.ID, leafB.ID, d2026, "li", "重新调查"); err != nil {
		t.Fatalf("normal reassign: %v", err)
	}
	// 同日再挂 → 挂重
	if _, err := pointRepo.AssignRegion(pA.ID, leafA.ID, d2026, "w", ""); !errors.Is(err, ErrDoubleAssignment) {
		t.Fatalf("same-date reassign want ErrDoubleAssignment, got %v", err)
	}
	// 往回改 → 挂重
	if _, err := pointRepo.AssignRegion(pA.ID, leafA.ID, d2026.AddDate(0, 0, -1), "w", ""); !errors.Is(err, ErrDoubleAssignment) {
		t.Fatalf("backdate want ErrDoubleAssignment, got %v", err)
	}
	// 非叶子
	if _, err := pointRepo.AssignRegion(pA.ID, l2.ID, time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), "w", ""); !errors.Is(err, ErrRegionNotLeaf) {
		t.Fatalf("non-leaf assign want ErrRegionNotLeaf, got %v", err)
	}
	// 未来再调整正常
	if _, err := pointRepo.AssignRegion(pA.ID, leafA.ID, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), "w", ""); err != nil {
		t.Fatalf("future reassign: %v", err)
	}

	// ---- 按当时归属回看：已发出说法不变 ----
	atBefore := must(pointRepo.AssignmentAt(pA.ID, dBefore))
	if atBefore.RegionID == nil || *atBefore.RegionID != leafA.ID {
		t.Fatalf("as-of before change should be A, got %+v", atBefore)
	}
	atWin := must(pointRepo.AssignmentAt(pA.ID, dAfter))
	if atWin.RegionID == nil || *atWin.RegionID != leafB.ID {
		t.Fatalf("as-of 2026-03 should be B, got %+v", atWin)
	}
	if len(atWin.RegionPath) != 3 {
		t.Fatalf("region path should have 3 levels, got %d", len(atWin.RegionPath))
	}
	atLater := must(pointRepo.AssignmentAt(pA.ID, time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)))
	if atLater.RegionID == nil || *atLater.RegionID != leafA.ID {
		t.Fatalf("as-of 2026-08 should be A again, got %+v", atLater)
	}

	// ---- 异名同地合并 + 条数对账 ----
	pB := &models.SurveyPoint{Name: "白泥关口音" + suffix, Code: "BNK" + suffix, CountyID: county.ID}
	if err := pointRepo.Create(pB, leafB.ID); err != nil {
		t.Fatal(err)
	}
	// 给 B 挂一个发音人 + 词表 + 任务，验证下游条数
	sp := &models.Speaker{
		CodeName: "sp" + suffix, BirthYear: 1960, Gender: "male",
		DialectPointCode: "BNK" + suffix, SurveyPointID: &pB.ID,
	}
	var spID int64
	if err := db.QueryRow(
		`INSERT INTO speakers (code_name, birth_year, gender, dialect_point_code, survey_point_id)
		 VALUES ($1,$2,$3,$4,$5) RETURNING id`,
		sp.CodeName, sp.BirthYear, sp.Gender, sp.DialectPointCode, sp.SurveyPointID).Scan(&spID); err != nil {
		t.Fatal(err)
	}
	var wlID int64
	must0(db.QueryRow(`INSERT INTO wordlists (name, entries) VALUES ('wl` + suffix + `','[]') RETURNING id`).Scan(&wlID))
	if _, err := db.Exec(
		`INSERT INTO tasks (wordlist_id, speaker_id, kind) VALUES ($1,$2,'record')`, wlID, spID); err != nil {
		t.Fatal(err)
	}

	countsABefore := must(pointRepo.Counts(pA.ID))
	countsBBefore := must(pointRepo.Counts(pB.ID))
	if countsBBefore.Speakers != 1 || countsBBefore.Tasks != 1 {
		t.Fatalf("B counts wrong: %+v", countsBBefore)
	}

	// 跨县不能自动合并
	if _, err := pointRepo.MergePoints(pA.ID, pOther.ID); !errors.Is(err, ErrCountyMismatch) {
		t.Fatalf("cross-county merge want ErrCountyMismatch, got %v", err)
	}
	// 自己合自己
	if _, err := pointRepo.MergePoints(pA.ID, pA.ID); !errors.Is(err, ErrSamePoint) {
		t.Fatalf("self merge want ErrSamePoint, got %v", err)
	}

	res, err := pointRepo.MergePoints(pA.ID, pB.ID)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if !res.Balanced {
		t.Fatalf("merge counts not balanced: %+v", res)
	}
	if res.CountsAfter.Speakers != countsABefore.Speakers+countsBBefore.Speakers {
		t.Fatalf("speakers after %d want %d", res.CountsAfter.Speakers, countsABefore.Speakers+countsBBefore.Speakers)
	}
	if res.CountsAfter.Tasks != countsABefore.Tasks+countsBBefore.Tasks {
		t.Fatalf("tasks after %d want %d", res.CountsAfter.Tasks, countsABefore.Tasks+countsBBefore.Tasks)
	}
	if res.CountsAfter.SurveyPoints != 2 {
		t.Fatalf("survey_points after should count tombstone: %d", res.CountsAfter.SurveyPoints)
	}

	// 旧点 id 自动归并
	canonical, moved, err := pointRepo.Resolve(pB.ID)
	if err != nil || !moved || canonical != pA.ID {
		t.Fatalf("resolve moved=%v canonical=%d err=%v", moved, canonical, err)
	}
	// 用旧点 id 查条数 = 合并后总数
	countsViaOld := must(pointRepo.Counts(pB.ID))
	if countsViaOld.Speakers != res.CountsAfter.Speakers || countsViaOld.Tasks != res.CountsAfter.Tasks {
		t.Fatalf("counts via old id not merged: %+v vs %+v", countsViaOld, res.CountsAfter)
	}
	// 旧名进了保留点的别名 → 再用旧名能认出
	if dup, _ := pointRepo.FindConflict(pB.Name, county.ID); dup == nil || dup.ID != pA.ID {
		t.Fatalf("retired name should alias to kept point, got %+v", dup)
	}
	// 已合并的点不能再调整归属
	if _, err := pointRepo.AssignRegion(pB.ID, leafA.ID, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), "", ""); !errors.Is(err, ErrAlreadyMerged) {
		t.Fatalf("assign to merged point want ErrAlreadyMerged, got %v", err)
	}
	// 合并前的回看仍读旧点自己的行（B）
	atB := must(pointRepo.AssignmentAt(pB.ID, dAfter))
	if atB.RegionID == nil || *atB.RegionID != leafB.ID {
		t.Fatalf("old point history must stay readable, got %+v", atB)
	}

	// 无归属点首次挂区成功
	pC := &models.SurveyPoint{Name: "松烟镇" + suffix, Code: "SY" + suffix, CountyID: county.ID}
	if err := pointRepo.Create(pC, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := pointRepo.AssignRegion(pC.ID, leafA.ID, time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), "", ""); err != nil {
		t.Fatalf("first assignment on point without region: %v", err)
	}

	t.Log("all archive lifecycle assertions passed")
}

func must0(err error) {
	if err != nil {
		panic(err)
	}
}
