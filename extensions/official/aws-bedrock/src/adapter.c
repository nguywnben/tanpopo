#include "../../anthropic/src/common.h"

static const char *region_of(const jval_t *cfg, uint32_t *len) {
    const char *region = jobj_str(cfg, "aws_region", len);
    if (!region || *len == 0) {
        *len = 9;
        return "us-east-1";
    }
    return region;
}

static void emit_signing_instruction(strbuf_t *sb, const jval_t *cfg) {
    uint32_t rlen = 0;
    const char *region = region_of(cfg, &rlen);
    sb_puts(sb, "[{\"secret_slot_name\":\"aws_credentials\",\"target_location\":\"CREDENTIAL_TARGET_LOCATION_HOST_CRYPTO_SIGN\",\"field_name\":\"Authorization\",\"crypto_sign\":{\"algorithm\":\"SIGNING_ALGORITHM_AWS_SIGV4\",\"output_encoding\":\"SIGNATURE_ENCODING_HEX_LOWER\",\"aws_region\":");
    sb_put_json_strn(sb, region, rlen);
    sb_puts(sb, ",\"aws_service\":\"bedrock\",\"signed_headers\":[\"content-type\"]}}]");
}

static void append_path_model(strbuf_t *path, const char *model, uint32_t len) {
    static const char hex[] = "0123456789ABCDEF";
    for (uint32_t i = 0; i < len; i++) {
        unsigned char c = (unsigned char)model[i];
        bool safe = (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
                    (c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.' || c == '~';
        if (safe) {
            sb_putc(path, (char)c);
        } else {
            sb_putc(path, '%');
            sb_putc(path, hex[c >> 4]);
            sb_putc(path, hex[c & 15]);
        }
    }
}

static void append_chat_content(strbuf_t *dst, const jval_t *content, bool *first) {
    if (!content) return;
    if (content->kind == JK_STR) {
        sb_puts(dst, "{\"text\":");
        sb_put_json_strn(dst, content->str, content->len);
        sb_putc(dst, '}');
        *first = false;
        return;
    }
    if (content->kind == JK_ARR) {
        for (uint32_t i = 0; i < content->count; i++) {
            const jval_t *part = content->items[i];
            uint32_t tl = 0;
            const char *text = jobj_str(part, "text", &tl);
            if (!text) text = jobj_str(part, "content", &tl);
            if (!text || tl == 0) continue;
            if (!*first) sb_putc(dst, ',');
            *first = false;
            sb_puts(dst, "{\"text\":");
            sb_put_json_strn(dst, text, tl);
            sb_putc(dst, '}');
        }
    }
}

static void append_tool_result(strbuf_t *body, const jval_t *message) {
    uint32_t id_len = 0;
    const char *id = jobj_str(message, "tool_call_id", &id_len);
    uint32_t content_len = 0;
    const char *content = jobj_str(message, "content", &content_len);
    sb_puts(body, "{\"toolResult\":{\"toolUseId\":");
    sb_put_json_strn(body, id ? id : "", id_len);
    sb_puts(body, ",\"content\":[{\"text\":");
    sb_put_json_strn(body, content ? content : "", content_len);
    sb_puts(body, "}]}}");
}

static void append_tool_config(strbuf_t *body, const jval_t *tools) {
    if (!tools || tools->kind != JK_ARR || tools->count == 0) return;
    sb_puts(body, ",\"toolConfig\":{\"tools\":[");
    bool first = true;
    for (uint32_t i = 0; i < tools->count; i++) {
        const jval_t *tool = tools->items[i];
        const jval_t *fn = jobj_get(tool, "function");
        const jval_t *src = fn ? fn : tool;
        uint32_t nl = 0, dl = 0;
        const char *name = jobj_str(src, "name", &nl);
        if (!name || nl == 0) continue;
        const char *description = jobj_str(src, "description", &dl);
        const jval_t *schema = jobj_get(src, "parameters");
        if (!schema) schema = jobj_get(src, "input_schema");
        if (!first) sb_putc(body, ',');
        first = false;
        sb_puts(body, "{\"toolSpec\":{\"name\":");
        sb_put_json_strn(body, name, nl);
        if (description && dl) {
            sb_puts(body, ",\"description\":");
            sb_put_json_strn(body, description, dl);
        }
        sb_puts(body, ",\"inputSchema\":{\"json\":");
        if (schema && schema->kind == JK_OBJ) json_write_sb(body, schema);
        else sb_puts(body, "{\"type\":\"object\",\"properties\":{}}");
        sb_puts(body, "}}}");
    }
    sb_puts(body, "]}");
}

WASM_EXPORT("build_validation_probe")
uint64_t build_validation_probe(uint32_t ptr, uint32_t len) {
    jval_t *in = json_parse((const char *)(uintptr_t)ptr, len);
    uint32_t cfg_len = 0;
    const char *cfg_raw = jobj_str(in, "config_json", &cfg_len);
    jval_t *cfg = json_parse(cfg_raw, cfg_len);
    uint32_t region_len = 0;
    const char *region = region_of(cfg, &region_len);

    strbuf_t out;
    sb_init(&out, 512);
    sb_puts(&out, "{\"method\":\"POST\",\"target_url\":\"https://bedrock-runtime.");
    sb_putn(&out, region, region_len);
    sb_puts(&out, ".amazonaws.com/model/anthropic.claude-3-5-sonnet-20241022-v2%3A0/converse\",\"safe_headers\":{\"Content-Type\":\"application/json\",\"Accept\":\"application/json\"},\"body_bytes\":\"e30=\",\"credential_injections\":");
    emit_signing_instruction(&out, cfg);
    sb_putc(&out, '}');
    return pack_sb(&out);
}

WASM_EXPORT("verify_validation_result")
uint64_t verify_validation_result(uint32_t ptr, uint32_t len) {
    jval_t *in = json_parse((const char *)(uintptr_t)ptr, len);
    int64_t status = jobj_i64(in, "http_status", 500);
    strbuf_t out;
    sb_init(&out, 256);
    if (status >= 200 && status < 300) {
        sb_puts(&out, "{\"valid\":true,\"detected_model_ids\":[]}");
    } else {
        sb_puts(&out, "{\"valid\":false,\"failure_code\":\"provider_validation_failed\",\"sanitized_message\":\"Bedrock Converse validation failed.\"}");
    }
    return pack_sb(&out);
}

WASM_EXPORT("transform_request")
uint64_t transform_request(uint32_t ptr, uint32_t len) {
    jval_t *in = json_parse((const char *)(uintptr_t)ptr, len);
    uint32_t cfg_len = 0, req_len = 0;
    const char *cfg_raw = jobj_str(in, "config_json", &cfg_len);
    jval_t *cfg = json_parse(cfg_raw, cfg_len);
    uint8_t *req_raw = jobj_b64(in, "normalized_request_json", &req_len);
    jval_t *req = json_parse((const char *)req_raw, req_len);

    uint32_t region_len = 0, model_len = 0;
    const char *region = region_of(cfg, &region_len);
    const char *model = jobj_str(req, "model", &model_len);
    if (!model || model_len == 0) {
        model = "anthropic.claude-3-5-sonnet-20241022-v2:0";
        model_len = (uint32_t)xstrlen(model);
    }

    strbuf_t body;
    sb_init(&body, 2048);
    sb_puts(&body, "{\"messages\":[");
    const jval_t *messages = jobj_get(req, "messages");
    bool first_message = true;
    strbuf_t system;
    sb_init(&system, 256);
    if (messages && messages->kind == JK_ARR) {
        for (uint32_t i = 0; i < messages->count; i++) {
            const jval_t *message = messages->items[i];
            uint32_t role_len = 0;
            const char *role = jobj_str(message, "role", &role_len);
            if (role && (xstreq_n(role, role_len, "system") || xstreq_n(role, role_len, "developer"))) {
                const jval_t *content = jobj_get(message, "content");
                if (system.len && content && content->kind == JK_STR) sb_putc(&system, '\n');
                if (content && content->kind == JK_STR) sb_putn(&system, content->str, content->len);
                continue;
            }
            if (!first_message) sb_putc(&body, ',');
            first_message = false;
            if (role && xstreq_n(role, role_len, "tool")) {
                append_tool_result(&body, message);
                continue;
            }
            sb_puts(&body, "{\"role\":");
            sb_put_json_str(&body, role && xstreq_n(role, role_len, "assistant") ? "assistant" : "user");
            sb_puts(&body, ",\"content\":[");
            bool first_content = true;
            append_chat_content(&body, jobj_get(message, "content"), &first_content);
            const jval_t *calls = jobj_get(message, "tool_calls");
            if (calls && calls->kind == JK_ARR) {
                for (uint32_t j = 0; j < calls->count; j++) {
                    const jval_t *call = calls->items[j];
                    const jval_t *fn = jobj_get(call, "function");
                    uint32_t id_len = 0, name_len = 0, args_len = 0;
                    const char *id = jobj_str(call, "id", &id_len);
                    const char *name = jobj_str(fn, "name", &name_len);
                    const char *args = jobj_str(fn, "arguments", &args_len);
                    jval_t *args_json = args ? json_parse(args, args_len) : NULL;
                    if (!first_content) sb_putc(&body, ',');
                    first_content = false;
                    sb_puts(&body, "{\"toolUse\":{\"toolUseId\":");
                    sb_put_json_strn(&body, id ? id : "call_0", id ? id_len : 6);
                    sb_puts(&body, ",\"name\":");
                    sb_put_json_strn(&body, name ? name : "", name_len);
                    sb_puts(&body, ",\"input\":");
                    if (args_json && args_json->kind == JK_OBJ) json_write_sb(&body, args_json);
                    else sb_puts(&body, "{}");
                    sb_puts(&body, "}}");
                }
            }
            sb_putc(&body, ']');
            sb_putc(&body, '}');
        }
    }
    sb_putc(&body, ']');
    if (system.len) {
        sb_puts(&body, ",\"system\":[{\"text\":");
        sb_put_json_strn(&body, system.data, system.len);
        sb_puts(&body, "}]");
    }

    const jval_t *max_tokens = jobj_get(req, "max_tokens");
    if (!max_tokens) max_tokens = jobj_get(req, "max_completion_tokens");
    int64_t max_value = max_tokens && max_tokens->kind == JK_NUM ? parse_i64_str(max_tokens->str, max_tokens->len, 4096) : 4096;
    sb_puts(&body, ",\"inferenceConfig\":{\"maxTokens\":");
    sb_put_i64(&body, max_value);
    const jval_t *temperature = jobj_get(req, "temperature");
    if (temperature && temperature->kind == JK_NUM) {
        sb_puts(&body, ",\"temperature\":");
        sb_putn(&body, temperature->str, temperature->len);
    }
    const jval_t *top_p = jobj_get(req, "top_p");
    if (top_p && top_p->kind == JK_NUM) {
        sb_puts(&body, ",\"topP\":");
        sb_putn(&body, top_p->str, top_p->len);
    }
    const jval_t *stop = jobj_get(req, "stop");
    if (stop) {
        sb_puts(&body, ",\"stopSequences\":");
        if (stop->kind == JK_STR) {
            sb_putc(&body, '[');
            sb_put_json_strn(&body, stop->str, stop->len);
            sb_putc(&body, ']');
        } else json_write_sb(&body, stop);
    }
    sb_putc(&body, '}');
    append_tool_config(&body, jobj_get(req, "tools"));
    sb_putc(&body, '}');

    strbuf_t path;
    sb_init(&path, 512);
    sb_puts(&path, "model/");
    append_path_model(&path, model, model_len);
    bool streaming = jobj_bool(req, "stream", false);
    sb_puts(&path, streaming ? "/converse-stream" : "/converse");

    strbuf_t target;
    sb_init(&target, 768);
    uint32_t base_len = 0;
    const char *base = get_config_base_url(cfg, "", &base_len);
    if (base && base_len > 0) {
        sb_putn(&target, base, base_len);
        while (target.len && target.data[target.len - 1] == '/') target.data[--target.len] = 0;
    } else {
        sb_puts(&target, "https://bedrock-runtime.");
        sb_putn(&target, region, region_len);
        sb_puts(&target, ".amazonaws.com");
    }
    sb_putc(&target, '/');
    sb_putn(&target, path.data, path.len);

    strbuf_t out;
    sb_init(&out, 2048 + body.len);
    sb_puts(&out, "{\"method\":\"POST\",\"target_url\":");
    sb_put_json_strn(&out, target.data, target.len);
    sb_puts(&out, streaming
        ? ",\"safe_headers\":{\"Content-Type\":\"application/json\",\"Accept\":\"application/vnd.amazon.eventstream\"},\"body_bytes\":"
        : ",\"safe_headers\":{\"Content-Type\":\"application/json\",\"Accept\":\"application/json\"},\"body_bytes\":");
    sb_put_b64(&out, (const uint8_t *)body.data, body.len);
    sb_puts(&out, ",\"credential_injections\":");
    emit_signing_instruction(&out, cfg);
    sb_putc(&out, '}');
    return pack_sb(&out);
}

static const char *finish_reason(const char *reason, uint32_t len, bool tools) {
    if (xstreq_n(reason, len, "max_tokens")) return "length";
    if (xstreq_n(reason, len, "tool_use")) return "tool_calls";
    if (xstreq_n(reason, len, "guardrail_intervened") || xstreq_n(reason, len, "content_filtered")) return "content_filter";
    if (tools) return "tool_calls";
    return "stop";
}

WASM_EXPORT("transform_response")
uint64_t transform_response(uint32_t ptr, uint32_t len) {
    jval_t *in = json_parse((const char *)(uintptr_t)ptr, len);
    uint32_t raw_len = 0;
    uint8_t *raw = jobj_b64(in, "raw_response_body", &raw_len);
    jval_t *doc = json_parse((const char *)raw, raw_len);
    const jval_t *output = jobj_get(doc, "output");
    const jval_t *message = jobj_get(output, "message");
    const jval_t *content = jobj_get(message, "content");
    uint32_t model_len = 0, reason_len = 0;
    const char *model = jobj_str(doc, "model", &model_len);
    if (!model || !model_len) { model = "bedrock-model"; model_len = (uint32_t)xstrlen(model); }
    const char *reason = jobj_str(doc, "stopReason", &reason_len);
    strbuf_t text, calls;
    sb_init(&text, 512);
    sb_init(&calls, 512);
    uint32_t call_count = 0;
    if (content && content->kind == JK_ARR) {
        for (uint32_t i = 0; i < content->count; i++) {
            const jval_t *part = content->items[i];
            uint32_t tl = 0;
            const char *txt = jobj_str(part, "text", &tl);
            if (txt && tl) {
                if (text.len) sb_putc(&text, '\n');
                sb_putn(&text, txt, tl);
            }
            const jval_t *use = jobj_get(part, "toolUse");
            if (use) {
                uint32_t idl = 0, nl = 0;
                const char *id = jobj_str(use, "toolUseId", &idl);
                const char *name = jobj_str(use, "name", &nl);
                const jval_t *args = jobj_get(use, "input");
                strbuf_t args_json;
                sb_init(&args_json, 128);
                if (args) json_write_sb(&args_json, args); else sb_puts(&args_json, "{}");
                if (call_count) sb_putc(&calls, ',');
                call_count++;
                sb_puts(&calls, "{\"id\":");
                sb_put_json_strn(&calls, id ? id : "call_0", id ? idl : 6);
                sb_puts(&calls, ",\"type\":\"function\",\"function\":{\"name\":");
                sb_put_json_strn(&calls, name ? name : "", nl);
                sb_puts(&calls, ",\"arguments\":");
                sb_put_json_strn(&calls, args_json.data, args_json.len);
                sb_puts(&calls, "}}");
            }
        }
    }
    const jval_t *usage = jobj_get(doc, "usage");
    int64_t input = jobj_i64(usage, "inputTokens", 0);
    int64_t output_tokens = jobj_i64(usage, "outputTokens", 0);
    const char *finish = finish_reason(reason, reason_len, call_count > 0);

    strbuf_t normalized;
    sb_init(&normalized, 1024 + text.len + calls.len);
    sb_puts(&normalized, "{\"id\":\"bedrock-converse\",\"object\":\"chat.completion\",\"model\":");
    sb_put_json_strn(&normalized, model, model_len);
    sb_puts(&normalized, ",\"choices\":[{\"index\":0,\"message\":{\"role\":\"assistant\",\"content\":");
    sb_put_json_strn(&normalized, text.data, text.len);
    if (call_count) {
        sb_puts(&normalized, ",\"tool_calls\":[");
        sb_putn(&normalized, calls.data, calls.len);
        sb_putc(&normalized, ']');
    }
    sb_puts(&normalized, "},\"finish_reason\":");
    sb_put_json_str(&normalized, finish);
    sb_puts(&normalized, "}],\"usage\":{\"prompt_tokens\":");
    sb_put_i64(&normalized, input);
    sb_puts(&normalized, ",\"completion_tokens\":");
    sb_put_i64(&normalized, output_tokens);
    sb_puts(&normalized, ",\"total_tokens\":");
    sb_put_i64(&normalized, input + output_tokens);
    sb_puts(&normalized, "}}");

    strbuf_t out;
    sb_init(&out, normalized.len + 512);
    sb_puts(&out, "{\"normalized_response_json\":");
    sb_put_b64(&out, (const uint8_t *)normalized.data, normalized.len);
    sb_puts(&out, ",\"usage\":{\"input_tokens\":");
    sb_put_i64(&out, input);
    sb_puts(&out, ",\"output_tokens\":");
    sb_put_i64(&out, output_tokens);
    sb_puts(&out, ",\"cache_read_tokens\":0,\"cache_write_tokens\":0,\"reasoning_tokens\":0,\"accuracy\":\"USAGE_ACCURACY_EXACT\"},\"upstream_model_id\":");
    sb_put_json_strn(&out, model, model_len);
    sb_puts(&out, ",\"finish_reason\":");
    sb_put_json_str(&out, finish);
    sb_putc(&out, '}');
    return pack_sb(&out);
}

static uint32_t read_u32be(const uint8_t *p) {
    return ((uint32_t)p[0] << 24) | ((uint32_t)p[1] << 16) | ((uint32_t)p[2] << 8) | (uint32_t)p[3];
}

static uint32_t event_crc32(const uint8_t *data, uint32_t len) {
    uint32_t crc = 0xffffffffu;
    for (uint32_t i = 0; i < len; i++) {
        crc ^= data[i];
        for (uint32_t bit = 0; bit < 8; bit++) {
            uint32_t mask = 0u - (crc & 1u);
            crc = (crc >> 1) ^ (0xedb88320u & mask);
        }
    }
    return ~crc;
}

static void bedrock_emit_stream_frame(strbuf_t *frames, bool *first, const char *json, uint32_t json_len) {
    strbuf_t sse;
    sb_init(&sse, json_len + 16);
    sb_puts(&sse, "data: ");
    sb_putn(&sse, json, json_len);
    sb_puts(&sse, "\n\n");
    if (!*first) sb_putc(frames, ',');
    *first = false;
    sb_put_b64(frames, (const uint8_t *)sse.data, sse.len);
}

static bool bedrock_header_string(const uint8_t *headers, uint32_t headers_len, const char *wanted, const char **value, uint32_t *value_len) {
    uint32_t pos = 0;
    while (pos < headers_len) {
        uint32_t name_len = headers[pos++];
        if (pos + name_len + 1 > headers_len) return false;
        const uint8_t *name = headers + pos;
        pos += name_len;
        uint8_t type = headers[pos++];
        const uint8_t *v = NULL;
        uint32_t vlen = 0;
        switch (type) {
        case 0: case 1: vlen = 0; break;
        case 2: vlen = 1; break;
        case 3: vlen = 2; break;
        case 4: vlen = 4; break;
        case 5: case 8: vlen = 8; break;
        case 6:
            if (pos + 2 > headers_len) return false;
            vlen = ((uint32_t)headers[pos] << 8) | headers[pos + 1];
            pos += 2;
            if (pos + vlen > headers_len) return false;
            v = headers + pos; pos += vlen;
            break;
        case 7:
            if (pos + 2 > headers_len) return false;
            vlen = ((uint32_t)headers[pos] << 8) | headers[pos + 1];
            pos += 2;
            if (pos + vlen > headers_len) return false;
            v = headers + pos; pos += vlen;
            break;
        case 9: vlen = 16; break;
        default: return false;
        }
        if (pos + vlen > headers_len) return false;
        if (vlen && !v) { v = headers + pos; pos += vlen; }
        if (name_len == xstrlen(wanted) && xmemcmp(name, wanted, name_len) == 0 && type == 7) {
            *value = (const char *)v;
            *value_len = vlen;
            return true;
        }
    }
    return false;
}

WASM_EXPORT("transform_stream_chunk")
uint64_t transform_stream_chunk(uint32_t ptr, uint32_t len) {
    jval_t *in = json_parse((const char *)(uintptr_t)ptr, len);
    uint32_t state_len = 0, chunk_len = 0;
    uint8_t *state_raw = jobj_b64(in, "session_state", &state_len);
    uint8_t *chunk = jobj_b64(in, "raw_upstream_chunk", &chunk_len);
    bool eof = jobj_bool(in, "is_eof", false);
    jval_t *state = state_len ? json_parse((const char *)state_raw, state_len) : NULL;
    uint32_t carry_len = 0, id_len = 0, model_len = 0, finish_len = 0;
    uint8_t *carry = jobj_b64(state, "carry", &carry_len);
    const char *id = jobj_str(state, "id", &id_len);
    const char *model = jobj_str(state, "model", &model_len);
    const char *finish = jobj_str(state, "finish", &finish_len);
    if (!id || !id_len) { id = "bedrock-converse-stream"; id_len = 23; }
    if (!model || !model_len) { model = "bedrock-model"; model_len = 13; }
    if (!finish || !finish_len) { finish = "stop"; finish_len = 4; }
    bool role_sent = jobj_bool(state, "role_sent", false);
    bool completed = jobj_bool(state, "completed", false);
    int64_t input_tokens = jobj_i64(state, "input_tokens", 0);
    int64_t output_tokens = jobj_i64(state, "output_tokens", 0);
    int64_t tool_index = jobj_i64(state, "tool_index", -1);

    strbuf_t buffer;
    sb_init(&buffer, carry_len + chunk_len + 64);
    if (carry && carry_len) sb_putn(&buffer, (const char *)carry, carry_len);
    if (chunk && chunk_len) sb_putn(&buffer, (const char *)chunk, chunk_len);
    strbuf_t frames;
    sb_init(&frames, 512);
    sb_putc(&frames, '[');
    bool first_frame = true;
    uint32_t pos = 0;
    bool malformed = false;

    while (buffer.len - pos >= 12) {
        const uint8_t *frame = (const uint8_t *)buffer.data + pos;
        uint32_t total_len = read_u32be(frame);
        uint32_t headers_len = read_u32be(frame + 4);
        if (total_len < 16 || total_len > (1u << 20) || headers_len > total_len - 16) { malformed = true; break; }
        if (buffer.len - pos < total_len) break;
        if (event_crc32(frame, 8) != read_u32be(frame + 8) || event_crc32(frame, total_len - 4) != read_u32be(frame + total_len - 4)) { malformed = true; break; }

        const char *event_type = NULL;
        uint32_t event_type_len = 0;
        if (bedrock_header_string(frame + 12, headers_len, ":event-type", &event_type, &event_type_len)) {
            const uint8_t *payload = frame + 12 + headers_len;
            uint32_t payload_len = total_len - headers_len - 16;
            jval_t *event = json_parse((const char *)payload, payload_len);
            const jval_t *usage = jobj_get(event, "usage");
            if (event_type && xstreq_n(event_type, event_type_len, "messageStart")) {
                if (!role_sent) {
                    strbuf_t out;
                    sb_init(&out, 256);
                    sb_puts(&out, "{\"id\":"); sb_put_json_strn(&out, id, id_len);
                    sb_puts(&out, ",\"object\":\"chat.completion.chunk\",\"model\":"); sb_put_json_strn(&out, model, model_len);
                    sb_puts(&out, ",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"}}]}");
                    bedrock_emit_stream_frame(&frames, &first_frame, out.data, out.len);
                    role_sent = true;
                }
            } else if (event_type && xstreq_n(event_type, event_type_len, "contentBlockStart")) {
                const jval_t *start = jobj_get(event, "start");
                const jval_t *tool = jobj_get(start, "toolUse");
                if (tool) {
                    uint32_t tid_len = 0, name_len = 0;
                    const char *tid = jobj_str(tool, "toolUseId", &tid_len);
                    const char *name = jobj_str(tool, "name", &name_len);
                    tool_index++;
                    strbuf_t out;
                    sb_init(&out, 384);
                    sb_puts(&out, "{\"id\":"); sb_put_json_strn(&out, id, id_len);
                    sb_puts(&out, ",\"object\":\"chat.completion.chunk\",\"model\":"); sb_put_json_strn(&out, model, model_len);
                    sb_puts(&out, ",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":"); sb_put_i64(&out, tool_index);
                    sb_puts(&out, ",\"id\":"); sb_put_json_strn(&out, tid ? tid : "call_bedrock", tid ? tid_len : 12);
                    sb_puts(&out, ",\"type\":\"function\",\"function\":{\"name\":"); sb_put_json_strn(&out, name ? name : "", name_len);
                    sb_puts(&out, ",\"arguments\":\"\"}}]}}]}");
                    bedrock_emit_stream_frame(&frames, &first_frame, out.data, out.len);
                }
            } else if (event_type && xstreq_n(event_type, event_type_len, "contentBlockDelta")) {
                const jval_t *delta = jobj_get(event, "delta");
                uint32_t text_len = 0;
                const char *text = jobj_str(delta, "text", &text_len);
                if (text && text_len) {
                    strbuf_t out;
                    sb_init(&out, 256 + text_len);
                    sb_puts(&out, "{\"id\":"); sb_put_json_strn(&out, id, id_len);
                    sb_puts(&out, ",\"object\":\"chat.completion.chunk\",\"model\":"); sb_put_json_strn(&out, model, model_len);
                    sb_puts(&out, ",\"choices\":[{\"index\":0,\"delta\":{\"content\":"); sb_put_json_strn(&out, text, text_len);
                    sb_puts(&out, "}}]}");
                    bedrock_emit_stream_frame(&frames, &first_frame, out.data, out.len);
                }
                uint32_t args_len = 0;
                const jval_t *tool_delta = jobj_get(delta, "toolUse");
                const char *args = jobj_str(tool_delta, "input", &args_len);
                if (args && args_len && tool_index >= 0) {
                    strbuf_t out;
                    sb_init(&out, 256 + args_len);
                    sb_puts(&out, "{\"id\":"); sb_put_json_strn(&out, id, id_len);
                    sb_puts(&out, ",\"object\":\"chat.completion.chunk\",\"model\":"); sb_put_json_strn(&out, model, model_len);
                    sb_puts(&out, ",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":"); sb_put_i64(&out, tool_index);
                    sb_puts(&out, ",\"function\":{\"arguments\":"); sb_put_json_strn(&out, args, args_len);
                    sb_puts(&out, "}}]}}]}");
                    bedrock_emit_stream_frame(&frames, &first_frame, out.data, out.len);
                }
            } else if (event_type && xstreq_n(event_type, event_type_len, "messageStop")) {
                uint32_t reason_len = 0;
                const char *reason = jobj_str(event, "stopReason", &reason_len);
                finish = finish_reason(reason, reason_len, tool_index >= 0);
                finish_len = (uint32_t)xstrlen(finish);
                strbuf_t out;
                sb_init(&out, 256);
                sb_puts(&out, "{\"id\":"); sb_put_json_strn(&out, id, id_len);
                sb_puts(&out, ",\"object\":\"chat.completion.chunk\",\"model\":"); sb_put_json_strn(&out, model, model_len);
                sb_puts(&out, ",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":"); sb_put_json_str(&out, finish); sb_puts(&out, "}]}");
                bedrock_emit_stream_frame(&frames, &first_frame, out.data, out.len);
                completed = true;
            } else if (event_type && xstreq_n(event_type, event_type_len, "metadata")) {
                input_tokens = jobj_i64(usage, "inputTokens", input_tokens);
                output_tokens = jobj_i64(usage, "outputTokens", output_tokens);
            }
        }
        pos += total_len;
    }

    uint32_t remaining = buffer.len - pos;
    if (eof && remaining == 0 && completed && !malformed) {
        const char done[] = "[DONE]";
        bedrock_emit_stream_frame(&frames, &first_frame, done, (uint32_t)(sizeof(done) - 1));
    }
    sb_putc(&frames, ']');

    strbuf_t next_state;
    sb_init(&next_state, 384 + remaining);
    sb_puts(&next_state, "{\"carry\":");
    sb_put_b64(&next_state, (const uint8_t *)buffer.data + pos, remaining);
    sb_puts(&next_state, ",\"id\":"); sb_put_json_strn(&next_state, id, id_len);
    sb_puts(&next_state, ",\"model\":"); sb_put_json_strn(&next_state, model, model_len);
    sb_puts(&next_state, ",\"finish\":"); sb_put_json_str(&next_state, finish);
    sb_puts(&next_state, ",\"role_sent\":"); sb_puts(&next_state, role_sent ? "true" : "false");
    sb_puts(&next_state, ",\"completed\":"); sb_puts(&next_state, (completed && !malformed) ? "true" : "false");
    sb_puts(&next_state, ",\"input_tokens\":"); sb_put_i64(&next_state, input_tokens);
    sb_puts(&next_state, ",\"output_tokens\":"); sb_put_i64(&next_state, output_tokens);
    sb_puts(&next_state, ",\"tool_index\":"); sb_put_i64(&next_state, tool_index);
    sb_putc(&next_state, '}');

    strbuf_t out;
    sb_init(&out, frames.len + next_state.len + 256);
    if (eof && remaining == 0 && completed && !malformed) {
        sb_puts(&out, "{\"next_session_state\":null,");
    } else {
        sb_puts(&out, "{\"next_session_state\":");
        sb_put_b64(&out, (const uint8_t *)next_state.data, next_state.len);
        sb_putc(&out, ',');
    }
    sb_puts(&out, "\"normalized_sse_frames\":"); sb_putn(&out, frames.data, frames.len);
    if (completed && !malformed) {
        sb_puts(&out, ",\"incremental_usage\":{\"input_tokens\":"); sb_put_i64(&out, input_tokens);
        sb_puts(&out, ",\"output_tokens\":"); sb_put_i64(&out, output_tokens);
        sb_puts(&out, ",\"cache_read_tokens\":0,\"cache_write_tokens\":0,\"reasoning_tokens\":0,\"accuracy\":\"USAGE_ACCURACY_EXACT\"}");
    }
    sb_puts(&out, ",\"stream_completed\":"); sb_puts(&out, (eof && remaining == 0 && completed && !malformed) ? "true}" : "false}");
    return pack_sb(&out);
}

WASM_EXPORT("normalize_error")
uint64_t normalize_error(uint32_t ptr, uint32_t len) {
    jval_t *in = json_parse((const char *)(uintptr_t)ptr, len);
    int64_t status = jobj_i64(in, "http_status", 500);
    strbuf_t out;
    sb_init(&out, 512);
    const char *code = status == 401 || status == 403 ? "authentication_error" : status == 429 ? "rate_limit_exceeded" : status >= 500 ? "upstream_server_error" : "invalid_request_error";
    const char *type = status == 401 || status == 403 ? "authentication_error" : status == 429 ? "rate_limit_error" : "upstream_error";
    bool retryable = status == 429 || status >= 500;
    sb_puts(&out, "{\"canonical_code\":"); sb_put_json_str(&out, code);
    sb_puts(&out, ",\"error_type\":"); sb_put_json_str(&out, type);
    sb_puts(&out, ",\"retryable_pre_stream\":"); sb_puts(&out, retryable ? "true" : "false");
    sb_puts(&out, ",\"trigger_connection_reauth\":"); sb_puts(&out, status == 401 || status == 403 ? "true" : "false");
    sb_puts(&out, ",\"sanitized_public_message\":\"Bedrock Converse request failed.\"}");
    return pack_sb(&out);
}

WASM_EXPORT("extract_usage")
uint64_t extract_usage(uint32_t ptr, uint32_t len) {
    jval_t *in = json_parse((const char *)(uintptr_t)ptr, len);
    uint32_t raw_len = 0;
    uint8_t *raw = jobj_b64(in, "raw_response_or_trailer_bytes", &raw_len);
    jval_t *doc = json_parse((const char *)raw, raw_len);
    const jval_t *usage = jobj_get(doc, "usage");
    int64_t input = jobj_i64(usage, "inputTokens", -1);
    int64_t output = jobj_i64(usage, "outputTokens", -1);
    strbuf_t out;
    sb_init(&out, 256);
    if (input >= 0 && output >= 0) {
        sb_puts(&out, "{\"input_tokens\":"); sb_put_i64(&out, input);
        sb_puts(&out, ",\"output_tokens\":"); sb_put_i64(&out, output);
        sb_puts(&out, ",\"cache_read_tokens\":0,\"cache_write_tokens\":0,\"reasoning_tokens\":0,\"accuracy\":\"USAGE_ACCURACY_EXACT\"}");
    } else {
        sb_puts(&out, "{\"input_tokens\":0,\"output_tokens\":0,\"cache_read_tokens\":0,\"cache_write_tokens\":0,\"reasoning_tokens\":0,\"accuracy\":\"USAGE_ACCURACY_ESTIMATED\"}");
    }
    return pack_sb(&out);
}

WASM_EXPORT("inspect_native_request")
uint64_t inspect_native_request(uint32_t ptr, uint32_t len) {
    jval_t *in = json_parse((const char *)(uintptr_t)ptr, len);
    uint32_t path_len = 0;
    const char *path = jobj_str(in, "upstream_path", &path_len);
    strbuf_t out;
    sb_init(&out, 512);
    if (path && xstrstarts(path, path_len, "model/") && xstrfind(path, path_len, "/converse") >= 0) {
        sb_puts(&out, "{\"credential_injections\":");
        jval_t *cfg = json_parse("{}", 2);
        emit_signing_instruction(&out, cfg);
        sb_puts(&out, ",\"target_model\":\"bedrock-native\"}");
    } else {
        sb_puts(&out, "{\"credential_injections\":[],\"target_model\":\"\"}");
    }
    return pack_sb(&out);
}
