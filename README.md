# 方言语音采集与标注平台 API · Dialect Corpus Platform

> 类型：后端 API 服务｜难度：★★★★｜技术栈：**Go + Gin + PostgreSQL + Redis（asynq 任务队列）+ MinIO**

## 1. 一句话简介
给语言学研究者和方言保护项目用的众包录音后端：发任务 → 收录音 → 切分与转写 → 双人交叉校验 → 导出语料包。

## 2. 真实场景与痛点
- 高校方言调查还在用录音笔 + Excel：录音文件命名混乱，谁录的、哪个县、什么词表全靠手记。
- 一个词表要 100 个人念，众包收上来的音频质量参差（有噪音、读错、格式不一）。
- 标注一致性无法保证，一个音标符号两种写法，后期清洗成本极高。

## 3. 目标用户
- 高校语言学/方言学课题组、语保工程项目组。
- 地方文化馆做方言留存。

## 4. 核心功能（MVP）
1. **调查表（Wordlist）管理**：条目（汉字 / 普通话释义 / 国际音标参考 / 例句），支持按语义场分组。
2. **录音任务分发**：按「发音人 × 词表」生成任务；发音人档案（年龄段、性别、母语方言点、是否长期外出）。
3. **音频上传与预处理**：
   - 上传直传对象存储，校验采样率/时长/峰值电平常量；
   - 自动切分：按静音检测把一段长录切成逐条音频（`silencedetect` / VAD）；
   - 统一转码为 16kHz 单声道 WAV。
4. **转写与标注**：记录 IPA、声调、备注；支持自定义符号表，禁止自由文本乱写（前端/后端共用符号校验）。
5. **双人交叉校验**：每条音频必须有 2 名标注员独立转写，不一致进仲裁队列，由管理员裁定。
6. **语料导出**：导出 `audio/ + metadata.csv + lexicon.tsv` 打包 zip，附 README 说明字段。

## 5. 进阶功能
- 进度看板数据（按任务维度的完成率 JSON，不做可视化页面）。
- 质量控制报告：录音响度分布、总时长、漏读条目。
- 音标相似度辅助（编辑距离预筛明显不一致）。
- 版本化：词表或标注规范变更时（v1→v2）保留历史，不覆盖。

## 6. 接口设计（节选）
```
POST /api/v1/wordlists                    创建词表（条目数组，可带语义场）
POST /api/v1/speakers                     发音人档案
POST /api/v1/tasks                        生成录音/标注任务
POST /api/v1/recordings/upload-url        获取直传 URL（对象存储预签名）
POST /api/v1/recordings                   登记已上传录音（触发切分）
GET  /api/v1/segments?task_id=&status=    分段列表（待标注/待仲裁/已完成）
PUT  /api/v1/segments/{id}/annotation     提交转写（乐观锁 version）
POST /api/v1/segments/{id}/arbitrate      仲裁裁决
GET  /api/v1/exports                      创建导出任务（异步）
GET  /api/v1/exports/{job_id}             导出进度与下载链接
```

## 7. 数据模型
```sql
speaker(id, code_name /* 脱敏代号 */, birth_year, gender, dialect_point_code, occupation, years_away)
wordlist(id, name, version, entries jsonb)          -- 条目：{id, hanzi, gloss, ipa_ref, group}
task(id, wordlist_id, speaker_id, kind /* record|annotate */, assignee, status)
recording(id, task_id, object_key, duration_ms, sample_rate, peak_db, device, recorded_at, status)
segment(id, recording_id, entry_id, start_ms, end_ms, object_key, snr_db, status)
annotation(id, segment_id, annotator, ipa, tone, note, decision /* pending|accept|reject|arbitrated */, version)
arbitration(id, segment_id, winner_annotation_id, arbiter, reason, created_at)
export_job(id, filter jsonb, status, progress, output_key, created_at)
```

## 8. 关键实现点
- **并发标注**：`segment` 上放乐观锁 `version`，两人同时改时后者收到 409，前端提示「已被他人更新」。
- **一致性门槛**：不一致判定用规范化后的字符串比较（NFD 归一 + 去空白），并支持自定义等价组（如 `ɿ` ≈ `z̩`）。
- **切分流水线**：asynq（基于 Redis 的 Go 任务队列）分三级（transcode → vad_split → index），失败自动重试 3 次并写入死信表。
- **去重**：同一 `speaker + entry` 若重复提交，保留最新但旧版进历史表，便于回退。
- **隐私**：发音人真实姓名与联系方式仅存 `contact_ref`（外部系统 ID），平台内一律用代号。

## 9. 技术约束与性能
- 上传单文件上限 500MB，超过则要求分片；服务端不中转文件，只发预签名 URL。
- 导出任务异步执行，10 万条 segment 打包 < 5 分钟，且支持断点续跑。
- 时长/采样率校验在入库前完成，不合格录音直接标 `rejected` 并回传给上传者原因。

## 10. 验收标准
- 1000 条录音（共 5 小时）自动切分准确率人工抽检 > 95%，单条切分误差 < 150ms。
- 双人标注不一致时 100% 进仲裁队列，仲裁后不可再改（只能新增修订记录）。
- 导出包字段与 README 完全一致，`metadata.csv` 可被 ELAN/Praat 脚本直接读取。

## 11. 边界（刻意不做）
不做在线学习课程、不做社交/社区功能、不做语音识别商业 API 中转——只做**语料采集与标注流程**。

## 12. 交付与容器化

交付要求：镜像必须能 `docker compose up` 起容器，并对容器发起真实 HTTP 请求通过第 10 节验收；不接受只在本机 `go run` 演示。

### 12.1 Dockerfile（多阶段构建）

- `builder`：`golang:1.22-alpine`，先 `COPY go.mod go.sum` 再 `go mod download`，一次构建出 `cmd/api` 与 `cmd/worker` 两个二进制
- `runtime`：**本项目不能用 distroless**（要执行 ffmpeg 可执行文件）→ `alpine:3.20` + `apk add --no-cache ffmpeg ca-certificates tzdata`，建非 root 用户 `appuser` 后运行；镜像约 90~120MB
- **ffmpeg 必须装进运行层**（VAD 切分与 16kHz 转码依赖），这是本项目最容易漏的依赖，也是唯一必须用 alpine 而非 distroless 的原因
- 健康检查：`HEALTHCHECK CMD ["/app","health"]`，由二进制自带的 `health` 子命令向本机 `GET /healthz` 发请求

### 12.2 docker-compose 服务

- `api`（容器 `cc-053-api`）：`9053:8000`，`restart: unless-stopped`，用 `audio-data` 卷与 worker 共享临时音频
- `worker`（`cc-053-worker`）：**与 api 同镜像、不同入口**（`/worker`），并发由环境变量配置（转码/切分是 CPU 密集，默认 2 即可，避免打满宿主机），并限额 `cpus: 2` / `memory: 1G`
- `beat`（`cc-053-beat`，`profiles: [optional]`）：定时清理死信与临时文件，默认不启动
- `db`（`cc-053-db`）：`postgres:16-alpine`，映射 `5433:5432`，卷 `pgdata`
- `redis`（`cc-053-redis`）：`redis:7-alpine`，映射 `6380:6379`，作为 asynq 队列与结果存储（`--appendonly yes` 防任务丢失）
- `minio`（`cc-053-minio`）：映射 `9001:9000` / `9002:9001`，音频与导出包存储，卷 `miniodata`
- **大文件上传**
  - API 只发预签名 URL，不中转文件；上传超时与最大体积（500MB）在 API 侧显式配置
  - 直传方案下要放宽 Go HTTP 服务的读写超时（`ReadTimeout` / `WriteTimeout`），否则 500MB 上传会被中途掐断

### 12.3 迁移与运行约束

- **数据库迁移**：schema 由 `internal/database` 内置 SQL 在启动时执行（仓库里的 `migrations/001_init.sql` 仅供人工参考，不参与运行）；api 与 worker 都会各自执行一次
- **任务幂等与重试**：切分任务失败自动重试 3 次后进死信表；同一 `recording` 重复触发切分必须先清除旧 segment（幂等），否则会产出双份语料
- **健康检查**：`GET /healthz`（含 db / redis / minio）；worker 以队列积压与死信数量作为探针
- **资源与日志**：给 `worker` 设 CPU 上限，避免切分吃满宿主；日志全部 stdout；密钥走 `.env` + `secrets`

### 12.4 起停与容器内验收

```bash
cd cc-053/cc-053
cp .env.example .env            # 填 DB / Redis / MinIO 与对外地址
docker compose up -d --build    # 起 api + worker + db + redis + minio
curl http://localhost:9053/healthz
docker compose logs -f worker
docker compose down             # 加 -v 一并清数据卷
```

**端到端链路**：容器内跑通「创建词表 → 上传长录音 → 自动切分 → 双人标注 → 仲裁 → 导出 zip」，1000 条录音切分抽检 > 95%；重启容器后任务队列不丢（Redis AOF 生效）；导出包字段与第 10 节一致。

### 12.5 忽略文件（.dockerignore / .gitignore）

交付时必须**同时**提供 `.dockerignore` 与 `.gitignore`，两者作用不同、缺一不可（`.gitignore` 对 `docker build` 无效，反之亦然）。

- **`.dockerignore`**（决定构建上下文）
  ```
  bin
  tmp
  .cache
  coverage.out
  .git
  .gitignore
  .env
  .env.*
  *.log
  coverage
  .vscode
  .idea
  Dockerfile
  docker-compose.yml
  README.md
  ```
  - **不要**忽略 `go.mod` / `go.sum`、`migrations/`、`*.sql`（依赖解析与入口 `migrate` 都需要）
  - **语音隐私红线**：任何真实录音、导出语料包（`*.wav`、`*.zip`、`data/audio/`、`exports/`）**必须写进忽略文件**，绝不入库、绝不进镜像——这些是发音人的敏感数据
  - 本地测试用的小样本音频放 `tests/fixtures/` 并控制在 1MB 内
- **`.gitignore`**
  ```
  bin/
  tmp/
  coverage.out
  *.test
  .env
  .env.local
  *.log
  coverage/
  .DS_Store
  .vscode/
  .idea/
  data/audio/
  exports/
  *.wav
  *.mp3
  ```
- **安全自检**：`git status` 与镜像内均无真实录音文件；`docker history` 无密钥；`.env.example` 保留而 `.env` 被忽略
