# adminui 替换为 Modra UI 的方案

状态：待评审。依据是对 `modra`、`feinian.ids`（已完成迁移的参考项目）和 `adminui` 三个仓库的调查，尚未在 yaginx 内实际试装。

## 1. 目标与非目标

目标：
- 用 Modra（React + Tailwind v4 + Radix）替换 `adminui` 的 antd 5、ProComponents、ProLayout 和 Less。
- 保持后端 API、路由路径、鉴权流程和部署产物（`adminui/dist`）不变。
- 界面支持亮/暗主题，并为中英文做好基础。

非目标：
- 不改后端和 OpenAPI 契约。
- 不新增业务功能。TCP/stream 代理、证书管理页、日志查看器等不在本次范围。
- 本次不要求完成业务文案国际化，见 §8 待决事项。

## 2. 评估结论

Modra 能满足 adminui 的需求，前提是先解决分发问题。

| 现有用法 | Modra 对应 | 说明 |
|---|---|---|
| ProLayout 侧栏、顶栏 | `AppShell`、`Layout`、`Menu`、`PageContainer` | 满足 |
| antd Table、`ServerPagedTable` | `DataTable`、`Pagination`、`FilterBar`、`RowActions` | 满足，分页封装重写 |
| antd Form（含动态规则） | `Form`（react-hook-form）、`FormListEditor` | 满足，写法不同 |
| Drawer、Modal、Popconfirm | `Sheet`、`Dialog`、`confirm()` | 满足 |
| `notification`、`message` | `Toast`、`Toaster` | 满足 |
| Tabs、Tag、Alert、Descriptions、Collapse | 均有 | 满足 |
| 登录页 | `AuthLayout`（仅容器） | 登录表单自写 |
| 图标 | `@feinian/fontawesome-free-react`（Free 6.4.0，CC BY 4.0 / MIT） | 见 §4 前置条件 |
| 路由、鉴权、fetch | 无 | 保留现有实现 |
| 代码编辑器、日志查看器 | 无 | adminui 目前未使用，不影响 |

## 3. 迁移规模

- 依赖 antd 的源文件约 48 个（`grep` 统计，只含 `antd` 与 `@ant-design/*` 的 import）。
- Less 文件 10 个，需逐页改写为 Tailwind 工具类。
- `@ant-design/pro-components` 只有 `PageHeader` 一处用到，`pro-layout` 用于整体布局。
- 31 个测试文件都是纯逻辑测试，没有 DOM 测试，迁移后 UI 回归没有自动化兜底。
- 规模明显小于 feinian.ids（237 个文件一次性替换），适合直接原生迁移。

## 4. 前置条件（阻塞项，阶段 0 之前必须确定）

### 4.1 Modra 包的分发方式（已决定）

采用 feinian.ids 的做法：包仍发布在 `https://npm.feinian.net/`，该源对读取是匿名公开的，不需要 token。已验证 `@modra/react` 的元数据和 tarball 都能匿名下载（2026-10-21，`dev` 标签为 `0.0.162`）。

yaginx 的做法：
- `adminui/.npmrc` 增加 `@modra:registry=https://npm.feinian.net/` 和 `@feinian:registry=https://npm.feinian.net/`（已在 spike 分支完成）。
- 精确锁版本，使用 `dev` 标签发布的构建号，不用 `^`。
- CI 与 Docker 构建无需额外凭据。

仍需确认：
- 该源的匿名读取是长期策略，而不是临时放开。若以后改为需要认证，yaginx 的公开 CI 会失效。
- yaginx 的用户从源码构建时依赖这个源，需要在 `CONTRIBUTING.md` 里说明。

### 4.2 Modra 侧需要先完成的事项

- Font Awesome Free 包目前是 Modra 工作区里未提交、未发布的改动（`packages/fontawesome-free-react`、`.changeset/fontawesome-free-edition.md`）。需要提交并发布后 yaginx 才能使用。
- Modra 根目录没有 `LICENSE` 文件，仅 `package.json` 声明 MIT。需要补上。
- 图标 CC BY 4.0 需要署名：yaginx 要保留包内 `NOTICE`，并在第三方声明中列出。

## 5. 技术方案

### 5.1 依赖变更

新增：
- `@modra/react`、`@modra/tokens`、`@modra/tailwind-preset`，精确锁版本（Modra 是 `0.0.x` 构建号，不用 `^`）。
- `@feinian/fontawesome-free-react`。
- `tailwindcss`、`@tailwindcss/vite`（4.3.x）。
- Modra 的可选 peer，需显式安装：`@tanstack/react-table`、`react-hook-form`、`date-fns`、`react-day-picker`；按需增加 `cmdk`、`@tanstack/react-virtual`。
- `.npmrc` 增加 `auto-install-peers=false`，与 feinian.ids 保持一致，避免隐式 peer。

移除（阶段 3）：`antd`、`@ant-design/*`、`less`、`vite-plugin-imp`，以及 `less-vars-to-js` 等配套。

### 5.2 构建配置

- `vite.config.ts`：增加 `@tailwindcss/vite` 插件，配置 `resolve.dedupe: ['react', 'react-dom']`；顺手删除死代码（注释掉的 `createVitePlugins`、未使用的 `vite-plugin-imp`、gzip 与 legacy 开关）。
- 入口 CSS：`@import "tailwindcss"; @import "@modra/tailwind-preset";`，并用 `@source` 指向 `node_modules/@modra/react/dist`。漏掉 `@source` 会导致样式悄悄丢失，需要在 CI 里用构建产物检查兜底。
- `index.html`：favicon 的 `/vite.svg` 改为相对路径。

### 5.3 主题、品牌与密度

- 在 `<body>` 上设置 `data-density`，不要放在 `<html>`（会改变 rem 基准，弹层也继承不到。feinian.ids 踩过）。
- `AppearanceProvider` 保留 `system/light/dark` 三态和持久化，切换目标改为 Modra 的 `.dark` 类。
- 品牌色从 `#2575ff`（暗色 `#76a8ff`）映射到 Modra 的 brand token；优先复用现成 brand，不够再通过 `ThemeProvider` 覆盖。
- 保留已自托管的 Noto Sans SC、IBM Plex Mono、Space Grotesk 字体。

### 5.4 Modra 的 `ConfigProvider` 与 locale

- 顶层挂载 `ConfigProvider`，传入 `locale` 为 `zh-CN`，让组件内置文案（分页、空状态、日期）变中文。
- 同时挂载 `<Toaster />` 和确认对话框宿主。

### 5.5 薄共享层 `src/ui/`

不做 antd 兼容适配层（见 §7 风险）。只抽四类确有重复的封装，API 按 Modra 风格设计：

- `ServerPagedTable`：包 `DataTable`，接 `Page<T>` 与现有的分页参数。
- `FormField`：统一标签、校验错误和必填标记。
- `confirmAction`：包 `confirm()`，统一危险操作的文案与样式。
- 展示辅助：`LocalTime`、空值占位等，使用 Modra 现成的 `EmptyValue`、`DateTimeDisplay` 等，缺的才补。

### 5.6 非页面代码的改动点

- `src/requests/FetchJson.ts`：把 antd `notification` 换成 Modra `Toast`（需要可在组件树外调用的 toast API，阶段 0 确认）。
- `src/layouts/*`：用 `AppShell`、`Menu`、`PageContainer` 重写，`router/menu.ts` 继续作为菜单数据源，`MenuIcon.tsx` 改为映射 Free 图标。
- `Login.tsx`：用 `AuthLayout` 加自写表单。
- 图表：`EChartPanel.tsx` 先保留；是否换成 Modra `EChart` 取决于 echarts 版本验证（见 §6 R4）。

### 5.7 不变的部分

- 路由结构与 `HashRouter`、所有路径和重定向。
- `src/api`、`src/requests` 的协议层、token 刷新、Web Worker 定时器、zustand 状态。
- OpenAPI 类型生成（`pnpm api:generate` / `api:check`）。
- 部署：`adminui/dist` 仍由 `.deb`、Docker 和 CI 原样拷贝，`deploy/*`、`Dockerfile` 无需改动。

## 6. 风险登记

| 编号 | 风险 | 影响 | 应对 |
|---|---|---|---|
| R1 | 公开 CI 拉不到私有源 | 无法构建 | §4.1 先决 |
| R2 | 无 DOM 测试，视觉与交互回归无兜底 | 迁移后才发现问题 | 阶段 0 起补 Playwright 冒烟用例 |
| R3 | Modra 版本为构建号，升级可能带来破坏性变化 | 构建或样式回归 | 精确锁版本，升级单独提交；本地联调用 `pnpm.overrides` 或 `link:` 指向本地 Modra |
| R4 | echarts 版本：adminui 用 ^6.1，Modra 可选 peer 为 ^5.5 | `EChart` 封装可能不兼容 | 阶段 0 验证；不行就保留 `EChartPanel` |
| R5 | `@source` 漏配导致 Tailwind 类被剔除 | 样式丢失 | 构建后检查关键类是否出现在产物 CSS |
| R6 | 站点编辑页（六个分区、动态表单、负载均衡与转换规则）逻辑复杂 | 工期与回归 | 放在最后迁移；表单从 antd `shouldUpdate` 改为 react-hook-form `watch` |
| R7 | `DataTable` 对大数据量（请求历史）的性能未知 | 卡顿 | 阶段 2 第 4 批先验证，必要时用 `VirtualDataTable` |
| R8 | Free 图标缺少个别 `@ant-design/icons` 的对应项 | 图标不全 | 阶段 0 列映射清单，缺的用 lucide 补 |
| R9 | 暗色与品牌色在 Modra 下对比度不达标 | 可读性 | 用 Modra 的对比度检查工具核对 |
| R10 | 文案全为硬编码中文，之后做 i18n 要再改一遍 | 返工 | 见 §8 |

## 7. 为什么不做 antd 兼容适配层

feinian.ids 用适配层一次替换了 237 个文件，一周内反复出现问题，可作为反面参考：

- 适配层大量使用 `any`，类型检查拦不住错误。
- 语义被悄悄丢掉：`Typography copyable`、`Statistic valueStyle`、`Result` 副标题都失效过，`ErrorBoundary` 是假的。
- 迁移后又发了约 10 次 Modra 版本来修问题。
- 到现在仍有 146 个页面文件依赖适配层，需要长期收缩。

yaginx 只有约 48 个文件依赖 antd，直接原生迁移的总成本不高于适配层加后续收缩，而且没有长期债务。

## 8. 待决事项

1. ~~分发方案~~ 已决定，见 §4.1。
2. 是否同步做业务文案国际化。建议：若有此计划，在阶段 2 迁每个页面时直接把文案抽到 locale 文件，避免二次返工；若没有，只接入 Modra 的 `zh-CN`。
3. echarts 是否允许降到 5.x 以使用 Modra 的 `EChart`，还是保留 `EChartPanel`。
4. 是否保留 `HashRouter`。建议保留，改动面最小，与现有 `basePath` 约束一致。

## 9. 实施计划

### 阶段 0：可行性验证（约 1～2 天，分支 `feat/modra-spike`）

出口标准：以下全部通过才进入阶段 1。

- [x] Modra 包可从 `npm.feinian.net` 匿名安装。
- [ ] Free 图标包已发布到同一源，CI 能安装。
- [ ] 登录页与站点列表页用 Modra 渲染，数据来自真实 API。
- [ ] Tailwind v4 与 Vite 7 构建通过，`@source` 生效。
- [ ] echarts 6 与 `EChart`（或保留 `EChartPanel`）可用。
- [ ] `DataTable` 能承载服务端分页、列筛选、行选择。
- [ ] Free 图标按需导入（如 `icons/solid/house`），产物里没有整库。
- [ ] 全局 toast 能在组件外调用（给 `FetchJson` 用）。
- [ ] 输出现有 `@ant-design/icons` 到 Free 图标的映射清单。
- [ ] 补齐 Playwright 冒烟：登录、站点列表、站点创建、站点编辑。

### 阶段 0 执行结果（2026-10-21，分支 `feat/modra-spike`）

方法：用 `pnpm pack` 把本地 Modra 的 tokens、tailwind-preset、react 和 fontawesome-free-react 打成 tarball，装进 `adminui`，新增独立入口 `spike.html`（Tailwind 只在该入口加载，不影响现有 antd 页面）。数据是模拟的，没有连真实后端。

| 项 | 结果 |
|---|---|
| Tailwind v4 与 Vite 7 构建 | 通过，`pnpm build` 与 `tsc --noEmit` 均无错误 |
| 与现有 antd 页面共存 | 通过，主入口 CSS 不含 Tailwind，样式互不影响 |
| `DataTable` 服务端分页 | 通过，翻页、总数“共 137 条”、每页条数均正常 |
| zh-CN locale | 通过，分页与空状态文案为中文 |
| 组件外调用 toast | 通过，`toast()` 是模块级 API，可直接用于 `FetchJson.ts` |
| 暗色模式 | 通过，给 `<html>` 加 `.dark` 即生效 |
| Free 图标按需导入 | 通过，产物不含图标库其余部分 |
| antd 图标映射 | 现有用到的 14 个图标在 Free 6.4 里都有对应项 |
| echarts 6.1 与 Modra `EChart` | 能渲染并自适应宽度，初始化时有一条“容器宽高为 0”的警告，待排查 |
| 私有源分发 | 已解决，见 §4.1。已改为从 `npm.feinian.net` 安装 `@modra/*@0.0.162`，构建通过。仅 Free 图标包因尚未发布，暂用本地 tarball |
| echarts peer 版本 | `@modra/react` 声明 `echarts@^5.5.0`，yaginx 是 6.1.0，pnpm 会给出 unmet peer 警告，运行正常 |
| 真实 API 对接 | 未验证 |
| Playwright 冒烟用例 | 未做 |

新发现，需要纳入阶段 1：
- `tsconfig.json` 的 `moduleResolution` 必须从 `Node` 改为 `Bundler`，否则 TypeScript 找不到 Modra 与图标包的类型。已验证改后现有代码仍能通过类型检查。
- Tailwind 产物 CSS 约 488 KB（gzip 63 KB），偏大，阶段 1 要确认是否包含了不必要的 token 或字体，并核对 `@source` 范围。
- `<body>` 在暗色下背景是透明的，页面背景靠 `<html>`，阶段 1 设置 `data-density` 时要一并确认。

### 阶段 1：地基（约 3～4 天）

- 依赖、构建配置、入口 CSS、主题与密度、`ConfigProvider`、`Toaster`。
- `AppearanceProvider` 对接 Modra 主题。
- `FetchJson` 去 antd 化。
- `RootLayout`、`UserLayout`、顶栏、侧栏改用 `AppShell` 与 `Menu`。
- 搭起 `src/ui/` 的四个封装。
- 此阶段新旧 UI 可共存（antd 页面仍可运行），保证每次提交都可构建、可部署。

### 阶段 1 执行结果（2026-10-21，分支 `feat/modra-spike`，未提交）

已完成：
- 依赖：`@modra/*` 与 `lucide-react` 精确锁版本，`.npmrc` 指向 `npm.feinian.net`；移除 spike 页面；Free 图标包因未发布暂不使用，图标先用 `lucide-react`。
- 构建：`@tailwindcss/vite`、`src/styles/modra.css`（`@import "tailwindcss"` 加 Modra preset 加 `@source`），`tsconfig` 的 `moduleResolution` 改为 `Bundler`。
- 主题：`appearance.tsx` 在原 antd 主题外再包 Modra 的 `ThemeProvider`（沿用 `#2575ff` 品牌色）和 `ConfigProvider`（zh-CN），并同步 `<html>` 的 `.dark` 类。
- 请求层：`FetchJson.ts` 的 antd `notification` 换成 `src/ui/notify.ts`（Modra `toast`），`App.tsx` 挂载 `<Toaster />` 与 `<ConfirmDialogHost />`。
- 外壳：`UserLayout`、`AppTopbar`、`SidebarAccount` 重写为 Modra 的 `AppShell`、`Menu`、`DropdownMenu`、`Dialog`、`Descriptions`；`MenuIcon` 改为 `src/ui/menuIcons.tsx`（lucide 映射）。
- 共享层 `src/ui/`：`ServerDataTable`（包 `DataTable`，服务端分页与表头过滤）、`confirmAction`、`notify`、`menuIcons`、`serverPageRequest`（从旧 `ServerPagedTable` 抽出，旧文件改为引用它）。

验证：
- `tsc --noEmit`、`pnpm test`（32 个文件，102 个用例）、`pnpm build` 全部通过。
- 在浏览器里用模拟后端验证：未迁移的 antd 站点列表页能在 Modra 外壳里正常显示；侧栏分组、选中态、收起为图标态、浅/深色切换、账号菜单、会话信息、移动端抽屉导航均正常。登录页（antd）在引入 Tailwind 后外观不变。
- 没有验证：真实 API、Playwright 冒烟用例、`ServerDataTable` 在真实页面中的使用（阶段 2 的站点列表页迁移时验证）。

与方案的偏差：
- 没有做 `FormField` 封装。Modra 自带 `Form`、`FormField`、`FormItem`、`FormGrid` 等，再包一层没有收益。
- 旧的外壳样式（`global.less`、`responsive.less` 里的 `.admin-shell`、`.app-*`、`.sider-menu` 等）和 `AppFooter.tsx` 已无引用，留到阶段 3 删除。

新发现：
- 主入口 CSS 增大到约 513 KB（gzip 69 KB）。其中约 75% 是 Modra 令牌变量，因为 preset 会引入全部品牌（ocean、forest、ids）和密度（default、compact、admin）。建议在 Modra 侧提供只含默认品牌的令牌入口，或让 yaginx 只引需要的部分。
- 本地开发时 `config/constant.ts` 的 `VITE_PROXY_HTTP` 是占位值 `https:///`，任何 `/api` 请求都会让 Vite 开发服务器崩溃（`Invalid URL`）。这是迁移前就存在的问题，建议改为读环境变量。
- 本次验证用了一个临时的模拟后端和对 `constant.ts` 的临时修改，均已还原，没有留在仓库里。

### 阶段 2：按页面迁移（约 2～3 周）

按复杂度从低到高，每批一个 PR，PR 内删除对应 `.less` 并补 Playwright 用例：

1. 登录、账号、关于。
2. 系统设置：运行时、通知、凭据与集成。
3. 节点、ACME、IP 黑名单。
4. 请求历史、监控与流量、数据库性能（验证图表与大表格）。
5. 站点管理：列表、创建、编辑及六个分区（hosts、routes、transforms、responses、access、history）。
6. 仪表盘 Home。

### 阶段 3：收尾（约 2～3 天）

- 移除 antd 及相关依赖、Less 配置、死代码，重命名 `componets` 与 `woker.js`。
- 更新 `README.md`、`README.zh-CN.md`、`CONTRIBUTING.md`、`docs/release-packaging.md` 的构建说明。
- 记录 `CHANGELOG.md`，补第三方声明（Font Awesome Free、Modra）。
- 清理 `dist` 体积对比：记录替换前（约 5.1 MB）与替换后。

## 10. 验收标准

- `pnpm api:check`、`pnpm test -- --run`、`pnpm run build` 全部通过，CI 的 adminui、Go 集成和 docker 三个 job 通过。
- 产物中不再包含 antd 和 Less 的任何依赖。
- 所有路由、重定向和 `/api` 调用行为与迁移前一致。
- 亮/暗/跟随系统三种主题下，所有页面的文字对比度和布局正常。
- 移动端宽度下，侧栏抽屉、表格横向滚动、表单布局可用。
- `.deb` 与 Docker 镜像中的 UI 能正常打开，升级路径（`upgrade.go`）不受影响。

## 11. 回滚策略

- 阶段 1 到 3 都在功能分支上，每个页面批次独立 PR，出问题只回滚该批次。
- 阶段 1 后新旧 UI 共存，直到阶段 3 才删除 antd。在此之前随时可以停止并保留现状。
- 若阶段 0 不通过，只丢弃 spike 分支，对主干无影响。

## 12. 参考

- `feinian.ids`：`docs/ui-modra-audit-2026-10.md`、`frontend-shared/src/modra-adapter.tsx`、`frontend-shared/src/styles.css`、迁移提交 `f2289ad`。
- `modra`：`docs/design-boundary.md`、`packages/react/package.json`、`packages/fontawesome-free-react/README.md`。
