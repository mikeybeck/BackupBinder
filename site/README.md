# BackupBinder marketing site

Static landing page, plus a handbook under [`docs/`](docs/). Design system: [DESIGN.md](DESIGN.md).

## Local preview

```sh
python3 -m http.server --directory site
# open http://127.0.0.1:8000/
```

## Deploy (Cloudflare Pages)

Project: **backupbinder** (Direct Upload). Production URL: https://backupbinder.pages.dev

Requires Node.js 22+ and a Wrangler login (`npx wrangler login`).

```sh
# from repo root
npx wrangler@4 pages deploy site --project-name=backupbinder --commit-dirty=true
```

`DESIGN.md` and `.impeccable/` are excluded via `.assetsignore`.

### Custom domain: backupbinder.com

Domain DNS is on Cloudflare. After the first deploy:

1. Open [Pages → backupbinder → Custom domains](https://dash.cloudflare.com/1e06ae5ededff90eb6760d6fcee25344/pages/view/backupbinder/domains)
2. Add `backupbinder.com` and `www.backupbinder.com`
3. Cloudflare will create the DNS records on the zone automatically when the zone is in the same account

Until those domains are attached, use https://backupbinder.pages.dev
