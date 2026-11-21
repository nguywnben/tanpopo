# Guia do Usuário do Tanpopo

**Tanpopo** é um AI Gateway e Hub do Model Context Protocol (MCP) auto-hospedado em binário único, projetado para roteamento determinístico, segurança de credenciais somente gravação, contabilidade FinOps exata em micro-USD e operação pronta para ambientes offline.

---

## 1. Início Rápido e Configuração Inicial

1. Inicie o servidor Tanpopo:
   ```bash
   tanpopo serve --role all-in-one --listen 127.0.0.1:8080
   ```
2. Na primeira execução, o Tanpopo imprime um **token de bootstrap** de uso único nos logs do servidor.
3. Abra `http://127.0.0.1:8080/setup` no navegador e conclua o assistente de 4 etapas:
   - **Conta do proprietário:** informe o token de bootstrap, e-mail do administrador, senha (mínimo de 12 caracteres), nome da organização e nome do primeiro projeto.
   - **Primeira conexão (opcional):** conecte um provedor comercial (OpenAI, Anthropic, Google Gemini ou compatível com OpenAI) ou um mecanismo local auto-hospedado (vLLM, Ollama). Endpoints locais exigem habilitar explicitamente o acesso à rede privada e listar CIDRs exatos sem curinga (por exemplo, `127.0.0.1/32`).
   - **Custódia da chave:** confirme que você fez backup da chave mestra de criptografia da instância (KEK). A perda da chave mestra torna irrecuperáveis os segredos criptografados do cofre.
   - **Revisão e inicialização:** conclua a configuração, guarde sua Chave Virtual de uso único com segurança e valide a primeira solicitação em streaming.

---

## 2. Idioma e Aparência

- **Web Console:** use o seletor de idioma na barra superior (ou nas telas de Configuração inicial e Login). **Auto** vem selecionado por padrão, escolhendo o primeiro idioma suportado nas preferências do navegador e recorrendo ao English quando não houver correspondência. Escolhas manuais persistem entre recarregamentos e você pode voltar para **Auto** a qualquer momento.
- **CLI:** passe `--lang <código>` (por exemplo, `tanpopo --lang pt --help`) ou defina `TANPOPO_LANG` / `LC_ALL` / `LC_MESSAGES` / `LANG`. Nomes de comandos, flags e saídas legíveis por máquina permanecem estáveis em todos os idiomas.
- **Erros e Alertas de API:** os endpoints do plano de controle em `/admin/v1/*` respeitam o cabeçalho `Accept-Language` para mensagens legíveis (`error.message`) e resumos de alertas operacionais, preservando `error.code`, `error.type` e esquemas JSON estáveis.
- **Operação Offline:** todos os 15 idiomas, fontes e ícones são empacotados no binário—nenhuma requisição externa a CDNs é feita em tempo de execução.

---

## 3. Fluxos Principais do Console

### Visão geral e Rastreamentos
- **Visão geral:** exibe solicitações ao vivo, taxa de erro, sobrecarga p95 do proxy, gasto liquidado em micro-USD, retenções ativas, implantações com disjuntor aberto e alertas ativos com links diretos de remediação.
- **Rastreamentos:** registra telemetria de execução somente de metadados (nenhum byte de prompt ou resposta é armazenado). Clique em qualquer solicitação para inspecionar a cascata por etapa e a precisão de medição (`Exato`, `Estimado` ou `Não medido`).

### Provedores, Modelos e Roteamento
- **Provedores:** sele chaves de API, credenciais AWS IAM, segredos HMAC de webhook ou certificados TLS no cofre somente gravação—o texto simples é enviado uma vez e nunca retornado. Vincule adaptadores a endpoints upstream e execute sondas ao vivo.
- **Modelos e Implantações:** declare modelos lógicos com capacidades e vincule-os a conexões de provedor com tabelas de tarifas exatas em inteiros **nano-USD** não negativos.
- **Rotas e Políticas:** configure roteamento determinístico (`Failover por prioridade`, `Ponderada`, `Round robin ponderado`, `Menor custo`, `Menor latência (EWMA)`). Use **Explicar rota** para simular decisões em modo dry-run sem chamadas upstream nem retenções de orçamento. Configure políticas monotônicas nos escopos de Organização, Projeto e Ambiente—escopos filhos só podem restringir os limites herdados.

### Extensões, Hub MCP e Playground
- **Extensões:** prepare pacotes a partir do Marketplace em cache offline ou por URI fixada com SHA-256 de 64 caracteres. Revise as diferenças de permissão antes de aprovar a ativação e coloque pacotes comprometidos em quarentena imediatamente para cortar fluxos ativos.
- **Hub MCP:** registre servidores MCP Streamable HTTP, sincronize ferramentas, fixe hashes de esquema RFC 8785 JCS, classifique níveis de risco (`read_only`, `mutating`, `destructive`) e conceda ferramentas a Chaves Virtuais específicas.
- **Playground:** teste prompts interativamente com uma chave efêmera com limite de orçamento restrita à aba atual. Copie trechos prontos para execução em cURL, Python e TypeScript (OpenAI SDK).

### Acesso, FinOps, Segurança e Operações
- **Acesso e Chaves:** emita Chaves Virtuais por ambiente, rotacione chaves com janela de carência de 24 horas ou revogue-as por drenagem gradual ou corte de emergência. Gerencie atribuições RBAC de 6 funções (`Operador`, `Proprietário da Org`, `Admin de Equipe`, `Revisor de Segurança`, `Leitor FinOps`, `Desenvolvedor`).
- **FinOps:** monitore gastos liquidados exatos, estimados, retenções pré-saída ativas e solicitações não medidas. Defina tetos rígidos em inteiros micro-USD e exporte relatórios de rateio em CSV/JSON.
- **Segurança:** revise aprovações humanas (HITL) para chamadas MCP destrutivas, acione ou desative Kill Switches de emergência em 4 níveis e verifique a cadeia criptográfica de auditoria RFC 8785 JCS.
- **Operações (função `Operador`):** acompanhe a prontidão `/readyz` e as épocas de snapshot do cluster, crie backups criptografados `.tpop.enc`, execute pré-verificações de atualização e registre webhooks assinados com HMAC.

---

## 4. Resumo de Referência da CLI

| Comando | Finalidade |
| --- | --- |
| `tanpopo serve` | Iniciar o servidor Tanpopo (`--role all-in-one`, `control-plane`, `data-plane` ou `worker`). |
| `tanpopo doctor` | Verificar binário, conectividade do banco de dados e permissões de armazenamento (`--json` suportado). |
| `tanpopo support bundle` | Coletar um pacote de diagnóstico sanitizado (nunca inclui segredos em texto simples nem prompts). |
| `tanpopo admin reset-owner` | Redefinir offline a senha do proprietário local da organização (exige `--yes-i-know-this-resets-owner-password`). |
| `tanpopo backup create / restore` | Criar ou restaurar um arquivo de backup criptografado `.tpop.enc`. |
| `tanpopo plugin install / upgrade / rollback / quarantine` | Gerenciar extensões fixadas por digest pela linha de comando. |
| `tanpopo upgrade preflight` | Validar compatibilidade ABI das extensões instaladas antes de atualizar o Tanpopo Core. |
| `tanpopo keys / routes / audit / kill-switch` | Operar recursos remotos do plano de controle via `/admin/v1`. |
