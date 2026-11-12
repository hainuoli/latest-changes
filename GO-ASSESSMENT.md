# Latest Changes Go 实现评估

## 结论

可以用 Go 实现。上游是一个 GitHub Webhook 服务，不依赖数据库、Redis、
外部任务队列或 FastAPI 特有的业务能力。建议建立独立的 Go 项目，以导入的
Python 源码和测试作为行为参考；不逐行翻译，不把 Python 运行时引入 Mino。

评估阶段完成了源码导入。随后已实现 Go 版第一版；当前实现和运行方式以 README.md 为准。
尚未部署或创建远端仓库。

## 来源与验证

- 上游： https://github.com/tiangolo/latest-changes-app
- 固定版本：`40ba35e399a1c799029280832840f57cc6bf1c24`
- 源码位置：`reference/upstream/`
- 原始 MIT 许可证：`reference/upstream/LICENSE`
- 32 个普通文件与固定版本归档逐字节一致；保留 1 个原始相对符号链接。
- 13 个 Python 文件通过语法解析；识别到 82 个测试函数，参数化展开后用例数会更多。
- 应用代码约 1,311 行；本次没有安装 Python 依赖或运行上游 pytest，不能声称原测试已通过。
- 没有导入 `.git`、原始远端设置或历史；没有创建 GitHub Fork。

## 功能映射

| 上游模块 | 职责 | Go 实现方式 |
| --- | --- | --- |
| `app/main.py` | HTTP 入口、验签、事件处理 | `net/http`、显式事件分派与输入校验 |
| `app/github.py` | App JWT、安装令牌、GitHub API | HTTP 客户端、RSA 签名、JSON；保留仓库与权限范围校验 |
| `app/automatic_labels.py` | YAML 配置、路径规则、优先级 | YAML 解析、递归 glob；保留排除规则与首次匹配语义 |
| `app/latest_changes.py` | 日志生成、文件更新、冲突重试 | 确定性 Markdown 文本变换、Contents API、SHA 乐观并发控制 |
| `app/models.py` | 数据模型与约束 | Go struct 加显式必填字段、枚举和大小验证 |
| `app/config.py` | 环境变量和私钥配置 | 启动时读取并校验环境变量 |

标准库覆盖 HTTP、JSON、HMAC、RSA、Base64 和日志。YAML 与递归 glob 建议使用
少量维护中的库。不能直接用 `filepath.Match` 替代 Python 的 `full_match`：
`**`、路径分隔符和排除规则的语义需要专门测试。

## 建议的第一版范围

1. 保留 `/health` 与 `/webhooks/github`，验证原始请求体的 HMAC-SHA256，限制请求大小。
2. 处理默认分支 PR 的 opened、edited、synchronize、reopened、labeled、unlabeled 和 closed 事件。
3. 兼容现有 `auto-labels` 配置、单分类标签约束与 `latest-changes/label` 状态。
4. 保留手动分类、release 跳过规则；自动分类必须覆盖所有变更路径，包括重命名前后的路径。
5. 合并后更新日志，保留历史章节，重复事件不重复追加；遇到 SHA 冲突重新读取、重算并有限重试。
6. 自定义提交默认仅为 `📝 Update release notes`，无正文；提交消息配置必须拒绝换行。
7. 使用环境变量配置 App Client ID、私钥和 Webhook Secret；单二进制或容器部署。

第一版可先服务 Mino。上游针对 `pyproject.toml` 的依赖语义分类是 Python 生态的
附加能力；如果暂不迁移，应明确标记不支持，不能宣称与上游完全等价。
`go.mod`、`go.sum` 继续通过路径规则分类即可。

建议的最小代码组织：

```text
cmd/latest-changes/main.go
internal/config/
internal/github/
internal/labels/
internal/notes/
internal/webhook/
```

先使用标准库 HTTP 服务，不引入 Mino 尚在演进的 API，不增加项目之间的发布依赖。
目录按实际代码量建立，避免预先增加空抽象层。

## 自定义与切换

建议首先仅增加一个 `commit-title` 配置项，默认单行 Gitmoji 标题。这个键是
拟议中的 Go 版扩展，官方 App 不支持；切换之前不要把它写入 Mino 的现有配置。
将来可增加日志路径和格式配置，但不在第一版建立模板语言或插件系统。

移除正文中的 `[skip ci]` 会改变 CI 触发行为。需要时，只针对主分支 push
过滤仅修改日志文件的提交；PR 检查继续运行，避免必需检查因路径过滤一直 pending。

切换时安装自建 App，并替换官方 App 的写入与状态来源；避免两个 App 同时处理
同一仓库。Ruleset 中的绕过身份及状态检查来源也必须随 App 身份一起更新。

## 生产前必须验证的行为

- 官方验签测试向量、错误签名、超大请求、缺失或非法字段。
- 安装令牌只覆盖目标仓库和所需权限；私钥不出现在日志中。
- PR 文件分页完整；旧事件不能把状态写到过时的 head SHA。
- 混合变更不误分类；手动选择、排除规则和优先级与约定一致。
- 日志章节顺序、历史保留、重复事件幂等、并发合并时无覆盖丢失。
- 提交消息严格为单行，GitHub API 写入消息不含正文或尾注。
- GitHub 要求 Webhook 在 10 秒内收到 2XX；多次 API 请求可能超时。生产设计需要
  有界处理和失败重投策略。若采用异步确认，应在确认前可靠保存任务；单纯启动
  goroutine 后立即返回并不能提供重启恢复或持久交付保证。

上游代码本身同步处理事件，记录 Delivery ID 但没有持久去重表。当前 Go 版已使用本地持久文件队列，先保存任务再返回 202，支持失败重试和重启恢复。
仅支持单实例和持久本地目录，无需 Redis 或分布式基础设施。

## 官方参考

- Go HTTP： https://pkg.go.dev/net/http
- Go HMAC 恒定时间比较： https://pkg.go.dev/crypto/hmac#Equal
- GitHub Webhook 最佳实践： https://docs.github.com/en/webhooks/using-webhooks/best-practices-for-using-webhooks

## 提交规则

所有由代理创建的提交都必须先展示改动、验证结果和拟用标题，逐次取得用户明确批准。
提交只有单行 Gitmoji 标题，不超过 72 字符，无正文、署名或其他尾注。
本次未创建任何提交；详细规则见项目根目录 `AGENTS.md`。
