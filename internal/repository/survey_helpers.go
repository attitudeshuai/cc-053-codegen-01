package repository

import (
	"strings"
	"unicode"

	"github.com/lib/pq"
)

// BizError 可当场指认的业务错误（HTTP 层据此返回 4xx 而非 500）
type BizError struct {
	Code    string // conflict | not_leaf | not_found | overlap | no_assignment | merged | bad_input
	Message string
	Detail  string
}

func (e *BizError) Error() string {
	if e.Detail != "" {
		return e.Message + ": " + e.Detail
	}
	return e.Message
}

func bizErr(code, msg, detail string) *BizError {
	return &BizError{Code: code, Message: msg, Detail: detail}
}

// 常见 SQL 错误码
const (
	pgUniqueViolation     = "23505"
	pgForeignKeyViolation = "23503"
	pgCheckViolation      = "23514"
)

func asPgError(err error) *pq.Error {
	if e, ok := err.(*pq.Error); ok {
		return e
	}
	return nil
}

// isUnique 判断是否唯一约束冲突，可附带约束名
func isUnique(err error, constraint ...string) bool {
	e := asPgError(err)
	if e == nil || string(e.Code) != pgUniqueViolation {
		return false
	}
	if len(constraint) == 0 {
		return true
	}
	for _, name := range constraint {
		if e.Constraint == name {
			return true
		}
	}
	return false
}

// variantFold 常见地名用字 繁体/异体 -> 规范简体。
// 只收录地名高频字，保证「同一个地方两种写法」能对上；未收字按原样参与指纹。
var variantFold = map[rune]rune{
	'臺': '台', '台': '台',
	'長': '长',
	'縣': '县', '鎮': '镇', '鄉': '乡', '村': '村',
	'裏': '里', '里': '里', '嶴': '岙',
	'龍': '龙', '馬': '马', '鳳': '凤', '華': '华',
	'東': '东', '頭': '头', '碼': '码', '莊': '庄', '橋': '桥',
	'嶺': '岭', '崗': '岗', '壩': '坝', '塢': '坞',
	'涇': '泾', '滙': '汇', '匯': '汇', '灣': '湾',
	'漁': '鱼', '鹽': '盐', '劉': '刘', '陳': '陈',
	'張': '张', '羅': '罗', '鄭': '郑', '謝': '谢',
	'許': '许', '韓': '韩', '馮': '冯', '鄧': '邓',
	'蕭': '萧', '蔣': '蒋', '蘇': '苏',
	'呂': '吕', '盧': '卢', '鍾': '钟', '譚': '谭',
	'陸': '陆', '賈': '贾', '韋': '韦', '鄒': '邹',
	'閆': '闫', '賀': '贺', '顧': '顾', '龔': '龚',
	'萬': '万', '錢': '钱', '嚴': '严', '湯': '汤',
	'餘': '余', '葉': '叶', '趙': '赵', '楊': '杨',
	'黃': '黄', '吳': '吴', '孫': '孙',
	'興': '兴', '國': '国', '慶': '庆', '寧': '宁',
	'廣': '广', '陽': '阳', '陰': '阴', '瀋': '沈',
	'鐵': '铁', '烏': '乌', '魯': '鲁', '齊': '齐',
	'開': '开', '蘭': '兰', '瀘': '泸', '達': '达',
	'場': '场', '鋪': '铺', '關': '关', '門': '门',
	'澤': '泽', '蓮': '莲', '瀏': '浏', '醴': '醴',
	'嶽': '岳', '峯': '峰', '渡': '渡', '陂': '陂',
}

// NormalizeName 生成名字指纹：
// 全角转半角、繁/异体归并、转小写、去掉空白标点与组合记号。
// 「长沙市 」「長沙市」「长沙，市」都得到同一指纹。
func NormalizeName(s string) string {
	var b strings.Builder
	for _, r := range s {
		// 全角 ASCII（！～）转半角，全角空格转普通空格（随后被丢弃）
		if r >= 0xFF01 && r <= 0xFF5E {
			r -= 0xFEE0
		}
		if canon, ok := variantFold[r]; ok {
			r = canon
		}
		r = unicode.ToLower(r)
		// 丢弃空白、标点、组合记号
		if unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.Is(unicode.Mn, r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
