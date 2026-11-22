# Tanpopo Guides Portal

Start with the persona journey that matches your goal, or read the end-user operational documentation in all 15 supported languages. Internal engineering, architecture, and governance documentation remains in English as the canonical source under [`docs/`](../README.md).

## Four-Journey Portal (`TP-IMP-0046`)

| Journey | Audience | Scope |
| --- | --- | --- |
| [**1. Quickstart & First Inference**](./quickstart.md) | Developers & Evaluators | `< 3m` standalone binary or Docker Compose boot, `/setup` bootstrap, OpenAI SDK & native `/native/*` inference |
| [**2. Extension Authoring SDK**](./plugin-authoring-sdk.md) | Plugin Authors | Tier-1 Declarative vs. Tier-2 Wasm sandboxing, offline cassette tests, reproducible `.tpkg`, N/N-1 ABI checks |
| [**3. Production HA, Backups & Air-Gap**](./production-ha-and-airgap.md) | Platform & SRE Teams | Split planes, PostgreSQL 16+ `FORCE RLS`, Valkey Cluster, `.tpop.enc` recovery, `INC-01..07` runbooks, `--offline` `.tpbundle` |
| [**4. Security, Vault/KMS & MCP/A2A Governance**](./security-and-mcp-governance.md) | Security & AI Governance | 3-tier Vault/KMS envelope encryption, Four-Eyes approval, `MCP 2025-03-26` JCS pinning & HITL, A2A sidecars |

## Available Languages (Role-Oriented User Guides)

| Language | Locale | Guide |
| --- | --- | --- |
| 中文(简体) | `zh-Hans` | [用户指南](./zh-Hans/user-guide.md) |
| 中文(繁體) | `zh-Hant` | [使用者指南](./zh-Hant/user-guide.md) |
| Deutsch | `de` | [Benutzerhandbuch](./de/user-guide.md) |
| English | `en` | [User Guide](./en/user-guide.md) |
| Español | `es` | [Guía del usuario](./es/user-guide.md) |
| Français | `fr` | [Guide utilisateur](./fr/user-guide.md) |
| Indonesia | `id` | [Panduan Pengguna](./id/user-guide.md) |
| Italiano | `it` | [Guida utente](./it/user-guide.md) |
| 日本語 | `ja` | [ユーザーガイド](./ja/user-guide.md) |
| 한국어 | `ko` | [사용자 가이드](./ko/user-guide.md) |
| Português | `pt` | [Guia do usuário](./pt/user-guide.md) |
| Русский | `ru` | [Руководство пользователя](./ru/user-guide.md) |
| ภาษาไทย | `th` | [คู่มือผู้ใช้](./th/user-guide.md) |
| Türkçe | `tr` | [Kullanıcı Kılavuzu](./tr/user-guide.md) |
| Tiếng Việt | `vi` | [Hướng dẫn sử dụng](./vi/user-guide.md) |
