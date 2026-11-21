# Guía del usuario de Tanpopo

**Tanpopo** es una pasarela de IA y centro de Protocolo de Contexto de Modelo (MCP) autoalojado en un único binario, diseñado para enrutamiento determinista, bóveda de credenciales de solo escritura, contabilidad exacta FinOps en micro-USD y operación sin conexión.

---

## 1. Inicio rápido y configuración inicial

1. Inicia el servidor de Tanpopo:
   ```bash
   tanpopo serve --role all --listen 127.0.0.1:8080
   ```
2. En el primer arranque, Tanpopo imprime un **token de arranque** de un solo uso en el registro del servidor.
3. Abre `http://127.0.0.1:8080/setup` en tu navegador y completa el asistente de 4 pasos:
   - **Cuenta propietaria:** Introduce el token de arranque, correo electrónico del administrador, contraseña (mínimo 12 caracteres), nombre de la organización y del primer proyecto.
   - **Primera conexión (opcional):** Conecta un proveedor comercial (OpenAI, Anthropic, Google Gemini o compatible con OpenAI) o un motor local autoalojado (vLLM, Ollama). Los destinos locales exigen habilitar acceso privado e indicar CIDR exactos sin comodín (por ejemplo, `127.0.0.1/32`).
   - **Custodia de clave:** Confirma que respaldaste la clave maestra de cifrado de la instancia (Master KEK). Su pérdida vuelve irrecuperables los secretos cifrados.
   - **Revisión e inicialización:** Completa la instalación, guarda de forma segura tu clave virtual de visualización única y verifica la primera solicitud en streaming.

---

## 2. Idioma y apariencia

- **Consola Web:** Usa el selector de idioma en la barra superior (y en las pantallas de Configuración e Inicio de sesión). **Auto** viene seleccionado por defecto y elige el primer idioma compatible de tu navegador (con respaldo en English). La elección manual se guarda tras recargar y puedes volver a **Auto** cuando desees.
- **CLI:** Usa `--lang <código>` (p. ej. `tanpopo --lang es --help`) o las variables `TANPOPO_LANG`, `LC_ALL`, `LC_MESSAGES`, `LANG`. Los nombres de comandos, opciones y salidas legibles por máquina se mantienen estables.
- **Errores de API y alertas:** Los puntos `/admin/v1/*` respetan `Accept-Language` para mensajes `error.message` y resúmenes de alertas, manteniendo intactos `error.code`, `error.type` y los esquemas JSON.
- **Modo sin conexión:** Los 15 idiomas, fuentes e iconos están integrados en el binario, sin solicitudes externas a CDN.

---

## 3. Flujos principales de la consola

### Resumen y Trazas
- **Resumen** muestra solicitudes en vivo, tasa de error, sobrecarga p95 del proxy, gasto liquidado en micro-USD, retenciones activas, despliegues con circuito abierto y alertas activas con enlaces directos de mitigación.
- **Trazas** registra telemetría exclusivamente de metadatos (sin prompts ni respuestas). Haz clic en cualquier solicitud para ver la cascada por etapas y la precisión de medición (`Exacto`, `Estimado` o `Sin medir`).

### Proveedores, Modelos y Enrutamiento
- **Proveedores:** Sella claves API, credenciales AWS IAM, secretos HMAC o certificados TLS en la bóveda de solo escritura. Vincula adaptadores y ejecuta sondas en vivo.
- **Modelos y despliegues:** Declara modelos lógicos con sus capacidades y asígnales tarifas enteras exactas en **nano-USD**.
- **Rutas y políticas:** Configura enrutamiento determinista (`Conmutación por prioridad`, `Ponderado`, `Round robin ponderado`, `Menor costo`, `Menor latencia (EWMA)`). Usa **Explicar ruta** en modo simulación sin tráfico saliente. Las políticas jerárquicas de Organización, Proyecto y Entorno solo pueden endurecer las reglas heredadas.

### Extensiones, Centro MCP y Playground
- **Extensiones:** Prepara paquetes desde el catálogo en caché o con URI + SHA-256 de 64 caracteres. Revisa las diferencias de permisos antes de activar y pon en cuarentena paquetes comprometidos cortando flujos al instante.
- **Centro MCP:** Registra servidores Streamable HTTP, sincroniza herramientas, fija hashes de esquema RFC 8785 JCS, clasifica niveles de riesgo (`read_only`, `mutating`, `destructive`) y concede permisos por clave virtual.
- **Playground:** Prueba prompts con una clave efímera con tope presupuestario y copia fragmentos listos para cURL, Python y TypeScript.

### Acceso, FinOps, Seguridad y Operaciones
- **Acceso y claves:** Emite claves virtuales por entorno, rótalas con 24 h de gracia o revócalas por drenaje controlado o corte de emergencia. Gestiona 6 roles RBAC.
- **FinOps:** Controla el gasto exacto, estimado, retenciones activas y solicitudes sin medir; define límites en micro-USD y exporta informes CSV/JSON.
- **Seguridad:** Gestiona aprobaciones humanas (HITL) para llamadas MCP destructivas, activa o levanta interruptores de corte de 4 niveles y verifica la cadena criptográfica de auditoría RFC 8785 JCS.
- **Operaciones (rol `OPERATOR`):** Supervisa `/readyz` y épocas del clúster, crea copias cifradas `.tpop.enc`, ejecuta comprobaciones previas de actualización y registra webhooks firmados con HMAC.

---

## 4. Resumen de comandos CLI

| Comando | Propósito |
| --- | --- |
| `tanpopo serve` | Iniciar el servidor Tanpopo (`--role all`, `control-plane` o `data-plane`). |
| `tanpopo doctor` | Verificar archivos locales, base de datos y salud del servicio (`--output json`). |
| `tanpopo support bundle` | Generar archivo de diagnóstico depurado (sin secretos ni prompts). |
| `tanpopo admin reset-owner` | Restablecer sin conexión la contraseña del propietario local. |
| `tanpopo backup create / verify / restore` | Crear, verificar y restaurar copias cifradas `.tpop.enc`. |
| `tanpopo plugin init / validate / test / pack` | Crear, probar offline y empaquetar extensiones `.tpkg`. |
| `tanpopo upgrade preflight` | Validar compatibilidad ABI antes de actualizar Tanpopo Core. |
| `tanpopo key / route / budget / audit / killswitch` | Consultar recursos remotos del plano de control mediante `/admin/v1`. |
