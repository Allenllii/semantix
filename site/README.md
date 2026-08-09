# Semantix — Official Website

The official landing page for [Semantix](https://github.com/Gnosil/semantix), a self-evolving agent kernel.

Built with **Next.js 16** (App Router) + **Tailwind CSS v4** + **shadcn/ui**, white + `#2F967F` theme. Design layout inspired by [reasonix.io](https://reasonix.io/).

> **静态导出模式**：本页为纯展示（无 API/动态数据），`next.config.ts` 使用 `output: "export"`——构建产物 `site/out/` 可由任意静态托管（Cloudflare Pages / Netlify / S3）直接服务。

## Development

```bash
npm install
npm run dev        # http://localhost:3000
```

## Checks

```bash
npm run typecheck  # tsc --noEmit
npm run lint       # eslint
npm run build      # production build → site/out/
npm run check      # all three
```

## Deploy — Cloudflare Pages（当前方案）

免费 + 全球 CDN + Git 集成。两种方式任选：

### A. 本地 CLI（最快）

```bash
# 一次性：登录（浏览器 OAuth）或设 CLOUDFLARE_API_TOKEN（Pages:Edit 权限）
npx wrangler login            # 或 export CLOUDFLARE_API_TOKEN=...

# 构建 + 部署（首次加 --project-name semantix 创建项目）
npm run build
npx wrangler pages deploy out --project-name semantix
# 产物 URL：https://<hash>.semantix.pages.dev → 绑定自定义域名
```

### B. Git 集成（push 自动部署，推荐长期）

1. Cloudflare Dashboard → **Workers & Pages** → **Create → Pages → Connect to Git**
2. 选 `Gnosil/semantix` 仓库，构建配置：
   - 构建命令：`npm ci && npm run build`（构建目录 `site/`，输出目录 `out/`）
   - 根目录：`site`
3. 每次 push 到 main 自动构建部署

## Custom domain（子域名方案）

公司域名 `ensureok.ai` 指向官网，**根域不动，用子域名**：

```
semantix.ensureok.ai
```

1. Cloudflare Pages → 项目 → **Custom domains → Set up a custom domain** → 填 `semantix.ensureok.ai`
2. **公司 DNS 加一条 CNAME**（需公司 IT 执行）：
   ```
   类型: CNAME
   主机: semantix
   值:   semantix.pages.dev
   ```
3. Cloudflare 自动签发 HTTPS 证书

> 若 DNS 托管在 Cloudflare 同一账号下，第 2 步可在 Dashboard 一键完成（无需找 IT）。

## Structure

```
site/
  src/
    app/           # layout, metadata, globals.css (design tokens), page assembly
    components/    # Nav, Hero, Features, Components, Roadmap, Community, Install, Footer
    lib/           # cn() utility
```
