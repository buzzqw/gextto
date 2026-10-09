// Gextto's cgo bridge to libtorrent.
//
// Build notes (P1.6): this file targets libtorrent >= 2.0. A few libtorrent 2.0
// APIs used here (half_open_limit, cache_size, cache_expiry, torrent_status::error,
// ...) are marked TORRENT_DEPRECATED upstream to steer new code toward their
// replacements, but they still work in every supported 2.0.x release and their
// replacements are not available across all the distributions we ship for. The
// deprecation warnings the compiler prints are expected and tracked, not build
// failures: when the minimum supported libtorrent moves past them, migrate the
// calls and drop this note.
#include <libtorrent/magnet_uri.hpp>
#include <libtorrent/address.hpp>
#include <libtorrent/bdecode.hpp>
#include <libtorrent/alert_types.hpp>
#include <libtorrent/create_torrent.hpp>
#include <libtorrent/read_resume_data.hpp>
#include <libtorrent/session.hpp>
#include <libtorrent/session_params.hpp>
#include <libtorrent/session_stats.hpp>
#include <libtorrent/settings_pack.hpp>
#include <libtorrent/torrent_flags.hpp>
#include <libtorrent/torrent_info.hpp>
#include <libtorrent/write_resume_data.hpp>

#include <algorithm>
#include <cctype>
#include <chrono>
#include <cstdint>
#include <cstring>
#include <deque>
#include <exception>
#include <filesystem>
#include <fstream>
#include <iterator>
#include <map>
#include <mutex>
#include <sstream>
#include <string>
#include <thread>
#include <unordered_map>
#include <cstdio>
#include <unordered_set>
#include <vector>

#include <fcntl.h>
#include <unistd.h>

#include "libtorrent_bridge.h"

namespace lt = libtorrent;

struct gextto_lt_session {
    explicit gextto_lt_session(unsigned short port_min, unsigned short port_max,
        int download_limit, int upload_limit, int active_downloads, int active_seeds,
        int active_limit, int connections_limit, bool dht, bool pex, bool lsd, bool upnp, bool natpmp)
        : pex_enabled(pex), session(make_params(port_min, port_max, download_limit, upload_limit,
            active_downloads, active_seeds, active_limit, connections_limit, dht, lsd, upnp, natpmp)) {}

    static lt::session_params make_params(unsigned short port_min, unsigned short port_max,
        int download_limit, int upload_limit, int active_downloads, int active_seeds,
        int active_limit, int connections_limit, bool dht, bool lsd, bool upnp, bool natpmp) {
        lt::settings_pack settings;
        settings.set_str(lt::settings_pack::listen_interfaces,
            "0.0.0.0:" + std::to_string(port_min) + "-" + std::to_string(port_max));
        settings.set_int(lt::settings_pack::download_rate_limit, download_limit);
        settings.set_int(lt::settings_pack::upload_rate_limit, upload_limit);
        settings.set_int(lt::settings_pack::active_downloads, active_downloads);
        settings.set_int(lt::settings_pack::active_seeds, active_seeds);
        settings.set_int(lt::settings_pack::active_limit, active_limit);
        settings.set_int(lt::settings_pack::connections_limit, connections_limit);
        settings.set_bool(lt::settings_pack::enable_dht, dht);
        settings.set_bool(lt::settings_pack::enable_lsd, lsd);
        settings.set_bool(lt::settings_pack::enable_upnp, upnp);
        settings.set_bool(lt::settings_pack::enable_natpmp, natpmp);
        // Without this the session only emits error alerts (the default mask is
        // 0x1): lifecycle events the wrapper relies on (metadata, finished,
        // storage moved, checked) and every tracker/file/peer error would be
        // silently dropped. Enable the categories we consume.
        const auto alert_categories =
            lt::alert_category::error | lt::alert_category::status | lt::alert_category::storage
            | lt::alert_category::tracker | lt::alert_category::connect
            | lt::alert_category::port_mapping | lt::alert_category::performance_warning
            | lt::alert_category::dht | lt::alert_category::ip_block | lt::alert_category::stats;
        settings.set_int(lt::settings_pack::alert_mask,
            static_cast<int>(static_cast<std::uint32_t>(alert_categories)));
        return lt::session_params(std::move(settings));
    }

    bool pex_enabled;
    bool sequential_enabled = false;
    // Hash del torrent "pinned": volutamente fuori dall'auto-gestione.
    std::string pinned_hash;
    // Directory where periodic resume saves are written (set by
    // gextto_lt_request_resume_save). The save_resume_data_alert answers are
    // written by collect_events, inside the normal alert flow, so no lifecycle
    // event is lost.
    std::string periodic_resume_dir;
    lt::session session;
    std::deque<gextto_lt_event> events;
    std::unordered_map<std::string, std::chrono::steady_clock::time_point> active_since;
    // Da quanto un download attivo è a 0 B/s: serve a declassare i "lenti"
    // (priorità bassa) quando c'è coda, senza affamarli: la priorità conta solo
    // in contesa, quindi senza coda restano attivi e riprovano.
    std::unordered_map<std::string, std::chrono::steady_clock::time_point> download_stalled_since;
    std::deque<std::pair<std::chrono::steady_clock::time_point, std::int64_t>> dynamic_rate_samples;
    std::chrono::steady_clock::time_point dynamic_last_change =
        std::chrono::steady_clock::now() - std::chrono::seconds(600);
    // Cooldown separato per la salita: quando la banda è inutilizzata bisogna
    // recuperare in fretta, non aspettare il cooldown di dieci minuti pensato
    // per evitare l'oscillazione in discesa.
    std::chrono::steady_clock::time_point dynamic_last_increase =
        std::chrono::steady_clock::now() - std::chrono::seconds(600);
    std::chrono::steady_clock::time_point dynamic_seed_state_since = std::chrono::steady_clock::now();
    bool dynamic_seed_state = false;
    bool dynamic_was_enabled = false;
    int dynamic_saturation = 0;
    int dynamic_underused = 0;
    // Latest session counters from a `session_stats_alert` (see
    // gextto_lt_session_stats).
    std::map<std::string, std::int64_t> last_session_stats;
    // ETA-based queue order from the last pass; the queue is only reordered
    // when it actually changes, to avoid queue-position thrashing.
    std::vector<std::string> last_queue_order;
    // When the pinned torrent was force-activated (auto-managed cleared).
    bool pinned_forced_active = false;
};

// Rust calls this bridge from the web server and several background tasks.
// Serialize every session API call: although libtorrent has its own worker
// threads, the wrapper also owns mutable alert/queue state.
static std::recursive_mutex LIBTORRENT_API_MUTEX;

static void set_error(char* output, size_t output_size, const std::string& message) {
    if (output == nullptr || output_size == 0) return;
    std::strncpy(output, message.c_str(), output_size - 1);
    output[output_size - 1] = '\0';
}

static void copy_string(char* output, size_t output_size, const std::string& value) {
    if (output == nullptr || output_size == 0) return;
    std::strncpy(output, value.c_str(), output_size - 1);
    output[output_size - 1] = '\0';
}

static std::string hex_hash(lt::torrent_handle const& handle) {
    // A hybrid torrent has two hashes. Magnets and Gextto's database are keyed
    // by BTIH (v1), while `info_hash().to_string()` may pick the v2 hash. Keep
    // the bridge stable by preferring v1 whenever it is available.
#if LIBTORRENT_VERSION_NUM >= 20000
    const auto hashes = handle.info_hashes();
    auto raw = hashes.has_v1() ? hashes.v1.to_string() : hashes.v2.to_string();
#else
    auto raw = handle.info_hash().to_string();
#endif
    static constexpr char hex[] = "0123456789abcdef";
    std::string result;
    result.reserve(raw.size() * 2);
    for (unsigned char byte : raw) {
        result.push_back(hex[(byte >> 4) & 0x0f]);
        result.push_back(hex[byte & 0x0f]);
    }
    return result;
}

static lt::torrent_handle find_torrent(gextto_lt_session* session, const char* hash) {
    if (session == nullptr || hash == nullptr) return {};
    for (auto const& handle : session->session.get_torrents()) {
        if (hex_hash(handle) == hash) return handle;
    }
    return {};
}

static bool write_atomic(const std::filesystem::path& path, const std::vector<char>& content, std::string& error);

static void collect_events(gextto_lt_session* session) {
    std::vector<lt::alert*> alerts;
    session->session.pop_alerts(&alerts);
    for (auto* alert : alerts) {
        // Answer to a periodic resume save: persist it so a crash or a forced
        // kill does not lose downloads added or progressed since the start.
        // A torrent removed meanwhile has an invalid handle and is skipped,
        // so it is never resurrected at the next start.
        if (auto* saved = lt::alert_cast<lt::save_resume_data_alert>(alert)) {
            if (!session->periodic_resume_dir.empty() && saved->handle.is_valid()) {
                try {
                    auto content = lt::write_resume_data_buf(saved->params);
                    std::string write_error;
                    write_atomic(std::filesystem::path(session->periodic_resume_dir) / (hex_hash(saved->handle) + ".fastresume"),
                        content, write_error);
                } catch (...) {
                }
            }
            continue;
        }
        // Session counters are not lifecycle events: snapshot them and move on.
        if (auto* stats = lt::alert_cast<lt::session_stats_alert>(alert)) {
            const auto metrics = lt::session_stats_metrics();
            const auto counters = stats->counters();
            for (auto const& metric : metrics) {
                if (metric.value_index >= 0 && metric.value_index < static_cast<int>(counters.size()))
                    session->last_session_stats[std::string(metric.name)] = counters[metric.value_index];
            }
            continue;
        }
        int kind = 0;
        lt::torrent_handle handle;
        const char* moved_path = nullptr;
        std::string message = alert->message();
        if (const auto* event = lt::alert_cast<lt::metadata_received_alert>(alert)) { kind = 1; handle = event->handle; handle.set_flags(lt::torrent_flags::auto_managed); }
        else if (const auto* event = lt::alert_cast<lt::torrent_finished_alert>(alert)) { kind = 2; handle = event->handle; }
        else if (const auto* event = lt::alert_cast<lt::storage_moved_alert>(alert)) { kind = 3; handle = event->handle; moved_path = event->storage_path(); }
        // Surface failed storage moves instead of dropping them silently: a
        // move can fail with `fail_if_exist` when the destination already
        // exists, leaving the torrent on the RAM disk forever.
        else if (const auto* event = lt::alert_cast<lt::storage_moved_failed_alert>(alert)) { kind = 4; handle = event->handle; }
        else if (const auto* event = lt::alert_cast<lt::torrent_checked_alert>(alert)) { kind = 5; handle = event->handle; }
        // Error alerts: without an explicit alert_mask these were delivered but
        // never surfaced. They carry a human-readable message via `message()`.
        else if (const auto* event = lt::alert_cast<lt::torrent_error_alert>(alert)) { kind = 6; handle = event->handle; }
        else if (const auto* event = lt::alert_cast<lt::file_error_alert>(alert)) { kind = 7; handle = event->handle; }
        else if (const auto* event = lt::alert_cast<lt::tracker_error_alert>(alert)) { kind = 8; handle = event->handle; }
        else if (const auto* event = lt::alert_cast<lt::metadata_failed_alert>(alert)) { kind = 9; handle = event->handle; }
        else if (const auto* event = lt::alert_cast<lt::hash_failed_alert>(alert)) { kind = 10; handle = event->handle; }
        else if (const auto* event = lt::alert_cast<lt::save_resume_data_failed_alert>(alert)) { kind = 11; handle = event->handle; }
        else if (const auto* event = lt::alert_cast<lt::torrent_removed_alert>(alert)) { kind = 12; handle = event->handle; }
        // Session-level errors have no torrent handle.
        else if (lt::alert_cast<lt::portmap_error_alert>(alert)) { kind = 13; }
        else if (lt::alert_cast<lt::session_error_alert>(alert)) { kind = 14; }
        if (kind == 0) continue;
        const bool session_level = (kind == 13 || kind == 14);
        if (!session_level && !handle.is_valid()) continue;
        gextto_lt_event output{};
        output.kind = kind;
        if (handle.is_valid()) {
            auto status = handle.status(lt::torrent_handle::query_name | lt::torrent_handle::query_save_path);
            copy_string(output.hash, sizeof(output.hash), hex_hash(handle));
            copy_string(output.name, sizeof(output.name), status.name);
            copy_string(output.save_path, sizeof(output.save_path), moved_path == nullptr ? status.save_path : moved_path);
        }
        copy_string(output.message, sizeof(output.message), message);
        session->events.push_back(output);
    }
}

static bool write_atomic(const std::filesystem::path& path, const std::vector<char>& content, std::string& error) {
    const auto temporary = path.string() + ".tmp";
    const int fd = ::open(temporary.c_str(), O_WRONLY | O_CREAT | O_TRUNC, 0600);
    if (fd < 0) { error = std::strerror(errno); return false; }
    size_t written = 0;
    while (written < content.size()) {
        const auto result = ::write(fd, content.data() + written, content.size() - written);
        if (result < 0) { error = std::strerror(errno); ::close(fd); ::unlink(temporary.c_str()); return false; }
        written += static_cast<size_t>(result);
    }
    if (::fsync(fd) != 0) { error = std::strerror(errno); ::close(fd); ::unlink(temporary.c_str()); return false; }
    if (::close(fd) != 0) { error = std::strerror(errno); ::unlink(temporary.c_str()); return false; }
    if (::rename(temporary.c_str(), path.c_str()) != 0) { error = std::strerror(errno); ::unlink(temporary.c_str()); return false; }
    const int dir = ::open(path.parent_path().c_str(), O_RDONLY | O_DIRECTORY);
    if (dir >= 0) { ::fsync(dir); ::close(dir); }
    return true;
}

static int load_ipfilter_into_session(gextto_lt_session* session, const std::string& path) {
    if (session == nullptr) return 0;
    std::ifstream filter_file(path);
    if (!filter_file) return 0;
    lt::ip_filter filter;
    std::string rule_line;
    int rules = 0;
    auto trim = [](std::string text) {
        auto not_space = [](unsigned char character) { return !std::isspace(character); };
        text.erase(text.begin(), std::find_if(text.begin(), text.end(), not_space));
        text.erase(std::find_if(text.rbegin(), text.rend(), not_space).base(), text.end());
        return text;
    };
    while (std::getline(filter_file, rule_line)) {
        auto comment = rule_line.find('#');
        if (comment != std::string::npos) rule_line.erase(comment);
        auto colon = rule_line.find_last_of(':');
        std::string range = colon == std::string::npos ? rule_line : rule_line.substr(colon + 1);
        auto dash = range.find('-');
        if (dash == std::string::npos) continue;
        try {
            auto first = lt::make_address(trim(range.substr(0, dash)));
            auto last = lt::make_address(trim(range.substr(dash + 1)));
            filter.add_rule(first, last, lt::ip_filter::blocked);
            ++rules;
        } catch (...) {
        }
    }
    if (rules > 0) {
        session->session.set_ip_filter(filter);
    }
    return rules;
}

extern "C" {
gextto_lt_session* gextto_lt_create(unsigned short port_min, unsigned short port_max,    int download_limit, int upload_limit, int active_downloads, int active_seeds,
    int active_limit, int connections_limit, unsigned char dht, unsigned char pex, unsigned char lsd,
    unsigned char upnp, unsigned char natpmp, char* error, size_t error_size) {
    try {
        return new gextto_lt_session(port_min, port_max, download_limit, upload_limit,
            active_downloads, active_seeds, active_limit, connections_limit,
            dht != 0, pex != 0, lsd != 0, upnp != 0, natpmp != 0);
    } catch (const std::exception& exception) {
        set_error(error, error_size, exception.what());
    } catch (...) {
        set_error(error, error_size, "unknown libtorrent session error");
    }
    return nullptr;
}

void gextto_lt_destroy(gextto_lt_session* session) {
    std::lock_guard<std::recursive_mutex> lock(LIBTORRENT_API_MUTEX);
    delete session;
}

void gextto_lt_version(char* output, size_t output_size) {
    copy_string(output, output_size, lt::version());
}

int gextto_lt_load_ipfilter(gextto_lt_session* session, const char* path, int* rules_out, char* error, size_t error_size) {
    std::lock_guard<std::recursive_mutex> lock(LIBTORRENT_API_MUTEX);
    if (session == nullptr || path == nullptr) {
        set_error(error, error_size, "invalid ip filter parameters");
        return 0;
    }
    try {
        int rules = load_ipfilter_into_session(session, std::string(path));
        if (rules_out != nullptr) *rules_out = rules;
        return 1;
    } catch (const std::exception& exception) {
        set_error(error, error_size, exception.what());
    } catch (...) {
        set_error(error, error_size, "unknown ip filter error");
    }
    return 0;
}

int gextto_lt_save_torrent(gextto_lt_session* session, const char* hash, const char* path, char* error, size_t error_size) {
    std::lock_guard<std::recursive_mutex> lock(LIBTORRENT_API_MUTEX);
    if (session == nullptr || hash == nullptr || path == nullptr) {
        set_error(error, error_size, "invalid torrent metadata save parameters");
        return 0;
    }
    try {
        auto handle = find_torrent(session, hash);
        if (!handle.is_valid()) { set_error(error, error_size, "torrent not found"); return 0; }
        auto info = handle.torrent_file();
        if (!info) { set_error(error, error_size, "torrent metadata unavailable"); return 0; }
        lt::create_torrent creator(*info);
        std::vector<char> content;
        lt::bencode(std::back_inserter(content), creator.generate());
        const std::filesystem::path target(path);
        std::filesystem::create_directories(target.parent_path());
        std::string write_error;
        if (!write_atomic(target, content, write_error)) {
            set_error(error, error_size, write_error);
            return 0;
        }
        return 1;
    } catch (const std::exception& exception) {
        set_error(error, error_size, exception.what());
    } catch (...) {
        set_error(error, error_size, "unknown torrent metadata save error");
    }
    return 0;
}

int gextto_lt_apply_settings(gextto_lt_session* session, const char* settings, char* error, size_t error_size) {
    std::lock_guard<std::recursive_mutex> lock(LIBTORRENT_API_MUTEX);
    if (session == nullptr || settings == nullptr) {
        set_error(error, error_size, "invalid libtorrent settings parameters");
        return 0;
    }
    try {
        lt::settings_pack pack;
        std::istringstream stream(settings);
        std::string line;
        int applied = 0;
        while (std::getline(stream, line)) {
            if (line.size() < 3 || line[1] != ':') continue;
            char type = line[0];
            auto separator = line.find('=', 2);
            if (separator == std::string::npos) continue;
            std::string key = line.substr(2, separator - 2);
            std::string value = line.substr(separator + 1);
            bool known = false;
            int number = 0;
            bool flag = value == "1" || value == "true" || value == "yes";
            if (type == 'i') {
                try { number = static_cast<int>(std::stoll(value)); } catch (...) { continue; }
            }
            if (key == "connections_limit") { pack.set_int(lt::settings_pack::connections_limit, number); known = true; }
            else if (key == "half_open_limit") { pack.set_int(lt::settings_pack::half_open_limit, number); known = true; }
            else if (key == "alert_queue_size") { pack.set_int(lt::settings_pack::alert_queue_size, number); known = true; }
            else if (key == "aio_threads") { pack.set_int(lt::settings_pack::aio_threads, number); known = true; }
            else if (key == "cache_size") { pack.set_int(lt::settings_pack::cache_size, number); known = true; }
            else if (key == "cache_expiry") { pack.set_int(lt::settings_pack::cache_expiry, number); known = true; }
            else if (key == "download_rate_limit") { pack.set_int(lt::settings_pack::download_rate_limit, number); known = true; }
            else if (key == "upload_rate_limit") { pack.set_int(lt::settings_pack::upload_rate_limit, number); known = true; }
            else if (key == "announce_interval") { pack.set_int(lt::settings_pack::min_announce_interval, number); known = true; }
            else if (key == "torrent_connect_boost") { pack.set_int(lt::settings_pack::torrent_connect_boost, number); known = true; }
            else if (key == "in_enc_policy") { pack.set_int(lt::settings_pack::in_enc_policy, number); known = true; }
            else if (key == "out_enc_policy") { pack.set_int(lt::settings_pack::out_enc_policy, number); known = true; }
            else if (key == "proxy_type") { pack.set_int(lt::settings_pack::proxy_type, number); known = true; }
            else if (key == "proxy_port") { pack.set_int(lt::settings_pack::proxy_port, number); known = true; }
            else if (key == "enable_utp") { pack.set_bool(lt::settings_pack::enable_outgoing_utp, flag); pack.set_bool(lt::settings_pack::enable_incoming_utp, flag); known = true; }
            else if (key == "prefer_rc4") { pack.set_bool(lt::settings_pack::prefer_rc4, flag); known = true; }
            else if (key == "announce_to_all_trackers") { pack.set_bool(lt::settings_pack::announce_to_all_trackers, flag); known = true; }
            else if (key == "announce_to_all_tiers") { pack.set_bool(lt::settings_pack::announce_to_all_tiers, flag); known = true; }
            else if (key == "allow_multiple_connections_per_ip") { pack.set_bool(lt::settings_pack::allow_multiple_connections_per_ip, flag); known = true; }
            else if (key == "apply_ip_filter") { pack.set_bool(lt::settings_pack::apply_ip_filter_to_trackers, flag); known = true; }
            else if (key == "proxy_host") { pack.set_str(lt::settings_pack::proxy_hostname, value); known = true; }
            else if (key == "proxy_username") { pack.set_str(lt::settings_pack::proxy_username, value); known = true; }
            else if (key == "proxy_password") { pack.set_str(lt::settings_pack::proxy_password, value); known = true; }
            else if (key == "listen_interfaces") { pack.set_str(lt::settings_pack::listen_interfaces, value); known = true; }
            // Killswitch VPN: forza il traffico in uscita su una scheda (estensione legacy).
            else if (key == "outgoing_interfaces") { pack.set_str(lt::settings_pack::outgoing_interfaces, value); known = true; }
            else if (key == "ip_filter_path") { load_ipfilter_into_session(session, value); known = true; }
            else if (key == "dht_bootstrap_nodes") { pack.set_str(lt::settings_pack::dht_bootstrap_nodes, value); known = true; }
            else if (key == "max_peerlist_size") { pack.set_int(lt::settings_pack::max_peerlist_size, number); known = true; }
            else if (key == "max_out_request_queue") { pack.set_int(lt::settings_pack::max_out_request_queue, number); known = true; }
            else if (key == "max_allowed_in_request_queue") { pack.set_int(lt::settings_pack::max_allowed_in_request_queue, number); known = true; }
            else if (key == "whole_pieces_threshold") { pack.set_int(lt::settings_pack::whole_pieces_threshold, number); known = true; }
            else if (key == "request_queue_time") { pack.set_int(lt::settings_pack::request_queue_time, number); known = true; }
            else if (key == "peer_connect_timeout") { pack.set_int(lt::settings_pack::peer_connect_timeout, number); known = true; }
            else if (key == "request_timeout") { pack.set_int(lt::settings_pack::request_timeout, number); known = true; }
            else if (key == "max_queued_disk_bytes") { pack.set_int(lt::settings_pack::max_queued_disk_bytes, number); known = true; }
            else if (key == "send_buffer_watermark") { pack.set_int(lt::settings_pack::send_buffer_watermark, number); known = true; }
            else if (key == "send_buffer_low_watermark") { pack.set_int(lt::settings_pack::send_buffer_low_watermark, number); known = true; }
            else if (key == "send_buffer_watermark_factor") { pack.set_int(lt::settings_pack::send_buffer_watermark_factor, number); known = true; }
            else if (key == "send_socket_buffer_size") { pack.set_int(lt::settings_pack::send_socket_buffer_size, number); known = true; }
            else if (key == "recv_socket_buffer_size") { pack.set_int(lt::settings_pack::recv_socket_buffer_size, number); known = true; }
            else if (key == "file_pool_size") { pack.set_int(lt::settings_pack::file_pool_size, number); known = true; }
            else if (key == "checking_mem_usage") { pack.set_int(lt::settings_pack::checking_mem_usage, number); known = true; }
            else if (key == "max_rejects") { pack.set_int(lt::settings_pack::max_rejects, number); known = true; }
            else if (key == "mixed_mode_algorithm") { pack.set_int(lt::settings_pack::mixed_mode_algorithm, number); known = true; }
            else if (key == "active_downloads") { pack.set_int(lt::settings_pack::active_downloads, number); known = true; }
            else if (key == "active_seeds") { pack.set_int(lt::settings_pack::active_seeds, number); known = true; }
            else if (key == "active_limit") { pack.set_int(lt::settings_pack::active_limit, number); known = true; }
            else if (key == "active_checking") { pack.set_int(lt::settings_pack::active_checking, number); known = true; }
            else if (key == "auto_manage_interval") { pack.set_int(lt::settings_pack::auto_manage_interval, number); known = true; }
            else if (key == "smooth_connects") { pack.set_bool(lt::settings_pack::smooth_connects, flag); known = true; }
            else if (key == "strict_end_game_mode") { pack.set_bool(lt::settings_pack::strict_end_game_mode, flag); known = true; }
            else if (key == "dont_count_slow_torrents") { pack.set_bool(lt::settings_pack::dont_count_slow_torrents, flag); known = true; }
            else if (key == "proxy_peer_connections") { pack.set_bool(lt::settings_pack::proxy_peer_connections, flag); known = true; }
            if (known) ++applied;
        }
        if (applied == 0) return 0;
        session->session.apply_settings(pack);
        return applied;
    } catch (const std::exception& exception) {
        set_error(error, error_size, exception.what());
    } catch (...) {
        set_error(error, error_size, "unknown libtorrent settings error");
    }
    return 0;
}

int gextto_lt_add(gextto_lt_session* session, const char* magnet, const char* save_path, char* error, size_t error_size) {
    std::lock_guard<std::recursive_mutex> lock(LIBTORRENT_API_MUTEX);
    if (session == nullptr || magnet == nullptr || save_path == nullptr) {
        set_error(error, error_size, "invalid libtorrent add parameters");
        return 0;
    }
    try {
        lt::error_code ec;
        lt::add_torrent_params params = lt::parse_magnet_uri(magnet, ec);
        if (ec) {
            set_error(error, error_size, ec.message());
            return 0;
        }
        params.save_path = save_path;
        if (!session->pex_enabled) params.flags |= lt::torrent_flags::disable_pex;
        if (session->sequential_enabled) params.flags |= lt::torrent_flags::sequential_download;
        session->session.add_torrent(std::move(params), ec);
        if (ec) {
            set_error(error, error_size, ec.message());
            return 0;
        }
        return 1;
    } catch (const std::exception& exception) {
        set_error(error, error_size, exception.what());
    } catch (...) {
        set_error(error, error_size, "unknown libtorrent add error");
    }
    return 0;
}

int gextto_lt_add_file(gextto_lt_session* session, const char* torrent_path, const char* save_path, char* hash, size_t hash_size, char* error, size_t error_size) {
    std::lock_guard<std::recursive_mutex> lock(LIBTORRENT_API_MUTEX);
    if (session == nullptr || torrent_path == nullptr || save_path == nullptr) {
        set_error(error, error_size, "invalid torrent-file add parameters");
        return 0;
    }
    try {
        lt::error_code ec;
        lt::add_torrent_params params;
        params.ti = std::make_shared<lt::torrent_info>(std::string(torrent_path), ec);
        if (ec) { set_error(error, error_size, ec.message()); return 0; }
        params.save_path = save_path;
        if (!session->pex_enabled) params.flags |= lt::torrent_flags::disable_pex;
        if (session->sequential_enabled) params.flags |= lt::torrent_flags::sequential_download;
        auto handle = session->session.add_torrent(std::move(params), ec);
        if (ec) { set_error(error, error_size, ec.message()); return 0; }
        copy_string(hash, hash_size, hex_hash(handle));
        return 1;
    } catch (const std::exception& exception) {
        set_error(error, error_size, exception.what());
    } catch (...) {
        set_error(error, error_size, "unknown torrent-file add error");
    }
    return 0;
}

// Add-time option flags shared by the `*_ex` entry points. The constants are
// declared in libtorrent_bridge.h so the Go layer and the bridge agree on the
// ABI.
static void gextto_apply_add_flags(lt::add_torrent_params& params, int flags) {
    if (flags & GEXTTO_ADD_PAUSED) params.flags |= lt::torrent_flags::paused;
    if (flags & GEXTTO_ADD_SEQUENTIAL) params.flags |= lt::torrent_flags::sequential_download;
    if (flags & GEXTTO_ADD_SEED_MODE) params.flags |= lt::torrent_flags::seed_mode;
    if (flags & GEXTTO_ADD_PREALLOCATE) params.storage_mode = lt::storage_mode_allocate;
    if (flags & GEXTTO_ADD_STOP_WHEN_READY) params.flags |= lt::torrent_flags::stop_when_ready;
}

int gextto_lt_add_ex(gextto_lt_session* session, const char* magnet, const char* save_path, int flags, char* error, size_t error_size) {
    std::lock_guard<std::recursive_mutex> lock(LIBTORRENT_API_MUTEX);
    if (session == nullptr || magnet == nullptr || save_path == nullptr) {
        set_error(error, error_size, "invalid libtorrent add parameters");
        return 0;
    }
    try {
        lt::error_code ec;
        lt::add_torrent_params params = lt::parse_magnet_uri(magnet, ec);
        if (ec) {
            set_error(error, error_size, ec.message());
            return 0;
        }
        params.save_path = save_path;
        if (!session->pex_enabled) params.flags |= lt::torrent_flags::disable_pex;
        if (session->sequential_enabled) params.flags |= lt::torrent_flags::sequential_download;
        gextto_apply_add_flags(params, flags);
        auto handle = session->session.add_torrent(std::move(params), ec);
        if (ec) {
            set_error(error, error_size, ec.message());
            return 0;
        }
        if (flags & GEXTTO_ADD_QUEUE_TOP) handle.queue_position_top();
        return 1;
    } catch (const std::exception& exception) {
        set_error(error, error_size, exception.what());
    } catch (...) {
        set_error(error, error_size, "unknown libtorrent add error");
    }
    return 0;
}

int gextto_lt_add_file_ex(gextto_lt_session* session, const char* torrent_path, const char* save_path, int flags, char* hash, size_t hash_size, char* error, size_t error_size) {
    std::lock_guard<std::recursive_mutex> lock(LIBTORRENT_API_MUTEX);
    if (session == nullptr || torrent_path == nullptr || save_path == nullptr) {
        set_error(error, error_size, "invalid torrent-file add parameters");
        return 0;
    }
    try {
        lt::error_code ec;
        lt::add_torrent_params params;
        params.ti = std::make_shared<lt::torrent_info>(std::string(torrent_path), ec);
        if (ec) { set_error(error, error_size, ec.message()); return 0; }
        params.save_path = save_path;
        if (!session->pex_enabled) params.flags |= lt::torrent_flags::disable_pex;
        if (session->sequential_enabled) params.flags |= lt::torrent_flags::sequential_download;
        gextto_apply_add_flags(params, flags);
        auto handle = session->session.add_torrent(std::move(params), ec);
        if (ec) { set_error(error, error_size, ec.message()); return 0; }
        copy_string(hash, hash_size, hex_hash(handle));
        if (flags & GEXTTO_ADD_QUEUE_TOP) handle.queue_position_top();
        return 1;
    } catch (const std::exception& exception) {
        set_error(error, error_size, exception.what());
    } catch (...) {
        set_error(error, error_size, "unknown torrent-file add error");
    }
    return 0;
}

unsigned int gextto_lt_torrent_count(const gextto_lt_session* session) {
    std::lock_guard<std::recursive_mutex> lock(LIBTORRENT_API_MUTEX);
    if (session == nullptr) return 0;
    return static_cast<unsigned int>(session->session.get_torrents().size());
}

size_t gextto_lt_statuses(const gextto_lt_session* session, gextto_lt_status* output, size_t capacity) {
    std::lock_guard<std::recursive_mutex> lock(LIBTORRENT_API_MUTEX);
    if (session == nullptr) return 0;
    auto statuses = session->session.get_torrent_status(
        [](lt::torrent_status const&) { return true; },
        lt::torrent_handle::query_name | lt::torrent_handle::query_save_path
            | lt::torrent_handle::query_accurate_download_counters);
    if (output == nullptr || capacity == 0) return statuses.size();
    const size_t count = std::min(capacity, statuses.size());
    for (size_t i = 0; i < count; ++i) {
        auto const& status = statuses[i];
        output[i] = {};
        copy_string(output[i].hash, sizeof(output[i].hash), hex_hash(status.handle));
        copy_string(output[i].name, sizeof(output[i].name), status.name);
        copy_string(output[i].save_path, sizeof(output[i].save_path), status.save_path);
        copy_string(output[i].error, sizeof(output[i].error), status.error);
        copy_string(output[i].current_tracker, sizeof(output[i].current_tracker), status.current_tracker);
        output[i].progress = static_cast<double>(status.progress) * 100.0;
        output[i].state = static_cast<int>(status.state);
        output[i].paused = static_cast<bool>(status.flags & lt::torrent_flags::paused) ? 1 : 0;
        output[i].download_rate = status.download_rate;
        output[i].upload_rate = status.upload_rate;
        output[i].download_payload_rate = status.download_payload_rate;
        output[i].upload_payload_rate = status.upload_payload_rate;
        output[i].num_peers = status.num_peers;
        output[i].num_seeds = status.num_seeds;
        output[i].num_complete = status.num_complete;
        output[i].num_incomplete = status.num_incomplete;
        output[i].num_connections = status.num_connections;
        output[i].connect_candidates = status.connect_candidates;
        output[i].download_limit = status.handle.download_limit();
        output[i].upload_limit = status.handle.upload_limit();
        output[i].all_time_upload = status.all_time_upload;
        output[i].all_time_download = status.all_time_download;
        output[i].seeding_seconds = status.seeding_duration.count();
        output[i].finished_time = std::chrono::duration_cast<std::chrono::seconds>(
            status.finished_duration).count();
        output[i].active_time = std::chrono::duration_cast<std::chrono::seconds>(
            status.active_duration).count();
        output[i].queue_position = static_cast<int>(status.queue_position);
        output[i].has_metadata = status.has_metadata ? 1 : 0;
        output[i].auto_managed = static_cast<bool>(status.flags & lt::torrent_flags::auto_managed) ? 1 : 0;
        output[i].is_seeding = status.is_seeding ? 1 : 0;
        output[i].sequential_download = static_cast<bool>(status.flags & lt::torrent_flags::sequential_download) ? 1 : 0;
        output[i].super_seeding = static_cast<bool>(status.flags & lt::torrent_flags::super_seeding) ? 1 : 0;
        output[i].upload_mode = static_cast<bool>(status.flags & lt::torrent_flags::upload_mode) ? 1 : 0;
        output[i].share_mode = static_cast<bool>(status.flags & lt::torrent_flags::share_mode) ? 1 : 0;
        output[i].distributed_copies = status.distributed_copies;
        output[i].torrent_version = 0;
        output[i].total_size = status.total_wanted;
        output[i].total_done = status.total_wanted_done;
        if (status.has_metadata) {
#if LIBTORRENT_VERSION_NUM >= 20000
            auto const& hashes = status.info_hashes;
            if (hashes.has_v1() && hashes.has_v2()) output[i].torrent_version = 3;
            else if (hashes.has_v2()) output[i].torrent_version = 2;
            else if (hashes.has_v1()) output[i].torrent_version = 1;
#else
            output[i].torrent_version = 1;
#endif
        }
    }
    return count;
}

size_t gextto_lt_events(gextto_lt_session* session, gextto_lt_event* output, size_t capacity) {
    std::lock_guard<std::recursive_mutex> lock(LIBTORRENT_API_MUTEX);
    if (session == nullptr) return 0;
    collect_events(session);
    if (output == nullptr || capacity == 0) return 0;
    const size_t count = std::min(capacity, session->events.size());
    for (size_t i = 0; i < count; ++i) {
        output[i] = session->events.front();
        session->events.pop_front();
    }
    return count;
}

void gextto_lt_promote_metadata(gextto_lt_session* session) {
    std::lock_guard<std::recursive_mutex> lock(LIBTORRENT_API_MUTEX);
    if (session == nullptr) return;
    const auto now = std::chrono::steady_clock::now();
    for (auto const& handle : session->session.get_torrents()) {
        auto status = handle.status();
        const auto hash = hex_hash(handle);
        if (!status.has_metadata && (status.flags & lt::torrent_flags::auto_managed) && (status.flags & lt::torrent_flags::paused)) {
            handle.unset_flags(lt::torrent_flags::auto_managed);
            handle.resume();
            session->active_since[hash] = now;
        }
    }
}

// Riarma l'auto-gestione sui torrent con metadati che non sono in pausa.
// Un torrent ripristinato da fastresume con `auto_managed` spento aggira per
// sempre `active_downloads`/`active_limit`, lasciando decine di download in
// parallelo. I torrent senza metadati sono esclusi di proposito: la fase di
// metadata-fetch deve poter ignorare la coda (vedi gextto_lt_promote_metadata).
// I torrent in pausa (scelta utente o politica) restano invariati.
size_t gextto_lt_ensure_auto_managed(gextto_lt_session* session) {
    std::lock_guard<std::recursive_mutex> lock(LIBTORRENT_API_MUTEX);
    if (session == nullptr) return 0;
    size_t changed = 0;
    for (auto const& handle : session->session.get_torrents()) {
        auto status = handle.status();
        if (!status.has_metadata) continue;
        if (status.flags & lt::torrent_flags::paused) continue;
        if (status.flags & lt::torrent_flags::auto_managed) continue;
        if (!session->pinned_hash.empty() && hex_hash(handle) == session->pinned_hash) continue;
        handle.set_flags(lt::torrent_flags::auto_managed);
        ++changed;
    }
    return changed;
}

// Nota: il riordino custom della coda è stato rimosso di proposito. Ordinava i
// torrent per rate/peer *live*, ma un torrent in coda (pausa) ha zero peer e
// zero rate, quindi finiva sempre in fondo: i torrent con fonti ma in attesa
// venivano affamati e la coda non ruotava più (`active_downloads` restava basso
// e l'aggregato crollava a zero). L'ordinamento e la rotazione sono lasciati
// all'auto-manager nativo di libtorrent, che non ha questo punto cieco.

void gextto_lt_adjust_queue(gextto_lt_session* session, int enabled, int static_downloads, int minimum, int maximum, int static_seeds, int static_limit, int global_download_limit) {
    std::lock_guard<std::recursive_mutex> lock(LIBTORRENT_API_MUTEX);
    if (session == nullptr) return;
    minimum = std::max(1, minimum);
    maximum = std::max(minimum, maximum);
    auto settings = session->session.get_settings();

    // Dynamic values belong to the live session, not to the configuration DB.
    // Disabling the feature must therefore restore the configured baseline and
    // remove the per-torrent overrides installed by the dynamic allocator.
    if (!enabled) {
        settings.set_int(lt::settings_pack::active_downloads, std::max(1, static_downloads));
        settings.set_int(lt::settings_pack::active_seeds, std::max(1, static_seeds));
        settings.set_int(lt::settings_pack::active_limit, std::max(1, static_limit));
        session->session.apply_settings(settings);
        if (session->dynamic_was_enabled) {
            for (auto const& handle : session->session.get_torrents()) {
                handle.set_max_connections(-1);
                handle.set_max_uploads(-1);
                handle.set_download_limit(0);
            }
        }
        session->dynamic_rate_samples.clear();
        session->dynamic_saturation = 0;
        session->dynamic_underused = 0;
        session->dynamic_was_enabled = false;
        return;
    }
    session->dynamic_was_enabled = true;

    const auto now = std::chrono::steady_clock::now();
    int downloads = settings.get_int(lt::settings_pack::active_downloads);
    int seeds = settings.get_int(lt::settings_pack::active_seeds);
    int active_limit = settings.get_int(lt::settings_pack::active_limit);
    std::int64_t aggregate_rate = 0;
    int queued_count = 0;
    bool priority_download = false;
    struct active_torrent { lt::torrent_handle handle; int tier; bool demoted; };
    std::vector<active_torrent> active;
    const auto stalled_limit = std::chrono::seconds(300);
    for (auto const& handle : session->session.get_torrents()) {
        auto status = handle.status();
        if (!status.has_metadata || status.is_seeding || status.is_finished) continue;
        const bool paused = static_cast<bool>(status.flags & lt::torrent_flags::paused);
        const int sources = status.num_peers + status.num_seeds;
        const auto hash = hex_hash(handle);
        if (paused) {
            ++queued_count;
            // Priorità media: può scalzare un attivo declassato quando c'è coda,
            // ma non un download che sta appena partendo.
            handle.set_priority(4);
            session->download_stalled_since.erase(hash);
            continue;
        }
        const int tier = status.download_rate > 0 ? 0 : (sources > 0 ? 1 : 2);
        aggregate_rate += std::max(0, status.download_rate);
        priority_download = priority_download || tier == 0;
        // Un download attivo ma fermo da troppo tempo viene "declassato": se
        // c'è coda l'auto-manager lo sospende in favore di un torrent con fonti.
        bool demoted = false;
        if (tier == 0) {
            session->download_stalled_since.erase(hash);
        } else {
            auto it = session->download_stalled_since.find(hash);
            if (it == session->download_stalled_since.end()) {
                session->download_stalled_since.emplace(hash, now);
            } else if (now - it->second >= stalled_limit) {
                demoted = true;
            }
        }
        active.push_back({handle, tier, demoted});
    }

    // Moving-window hysteresis per la discesa: campioni coerenti e cooldown di
    // dieci minuti evitano l'oscillazione su una linea instabile. La salita usa
    // un cooldown corto e non pretende di "vedere fonti": un torrent in coda è
    // in pausa e quindi ha sempre zero peer, non si può sapere se è scaricabile
    // finché non lo si attiva. Se la banda è inutilizzata e ci sono torrent in
    // attesa, si sale.
    session->dynamic_rate_samples.emplace_back(now, aggregate_rate);
    while (!session->dynamic_rate_samples.empty()
        && now - session->dynamic_rate_samples.front().first > std::chrono::seconds(600)) {
        session->dynamic_rate_samples.pop_front();
    }
    const bool decrease_ready = now - session->dynamic_last_change >= std::chrono::seconds(600);
    const bool increase_ready = now - session->dynamic_last_increase >= std::chrono::seconds(90);
    if (global_download_limit > 0 && session->dynamic_rate_samples.size() >= 3) {
        std::int64_t sum = 0;
        for (auto const& sample : session->dynamic_rate_samples) sum += sample.second;
        const double ratio = static_cast<double>(sum) / session->dynamic_rate_samples.size() / global_download_limit;
        if (ratio >= 0.9 && downloads > minimum && decrease_ready) {
            ++session->dynamic_saturation;
            session->dynamic_underused = 0;
            if (session->dynamic_saturation >= 3) {
                downloads = std::max(minimum, downloads - 1);
                session->dynamic_saturation = 0;
                session->dynamic_last_change = now;
            }
        } else if (queued_count > 0 && ratio <= 0.7 && downloads < maximum && increase_ready) {
            session->dynamic_underused = 0;
            session->dynamic_saturation = 0;
            // Salita più decisa quando la linea è quasi ferma. La fascia di
            // stabilità (0.7–0.9) lascia comunque margine prima della discesa.
            downloads = std::min(maximum, downloads + (ratio <= 0.4 ? 2 : 1));
            session->dynamic_last_increase = now;
        } else {
            session->dynamic_saturation = 0;
            session->dynamic_underused = 0;
        }
    } else if (global_download_limit <= 0 && queued_count > 0 && downloads < maximum && increase_ready) {
        session->dynamic_underused = 0;
        downloads = std::min(maximum, downloads + 1);
        session->dynamic_last_increase = now;
    } else {
        session->dynamic_saturation = 0;
        session->dynamic_underused = 0;
    }

    const bool seed_priority_state = queued_count > 0;
    if (seed_priority_state != session->dynamic_seed_state) {
        session->dynamic_seed_state = seed_priority_state;
        session->dynamic_seed_state_since = now;
    } else if (now - session->dynamic_seed_state_since >= std::chrono::seconds(300)) {
        seeds = seed_priority_state ? 1 : std::max(1, static_seeds);
    }
    int desired_limit = std::max(static_limit, downloads + seeds + 2);
    if (downloads >= settings.get_int(lt::settings_pack::active_downloads)
        && seeds >= settings.get_int(lt::settings_pack::active_seeds)) {
        desired_limit = std::max(active_limit, desired_limit);
    }
    settings.set_int(lt::settings_pack::active_downloads, downloads);
    settings.set_int(lt::settings_pack::active_seeds, seeds);
    settings.set_int(lt::settings_pack::active_limit, desired_limit);
    session->session.apply_settings(settings);

    // Give more of the global pool to torrents that are already transferring
    // or have sources ready, and keep seeding from starving a live download.
    if (!active.empty()) {
        const int connection_limit = std::max(1, settings.get_int(lt::settings_pack::connections_limit));
        const int upload_limit = std::max(1, settings.get_int(lt::settings_pack::unchoke_slots_limit));
        int total_weight = 0;
        for (auto const& item : active) {
            total_weight += item.tier == 0 ? 3 : (item.tier == 1 ? 2 : 1);
        }
        const int available_connections = std::max(static_cast<int>(connection_limit * 0.7), 10 * static_cast<int>(active.size()));
        const int available_uploads = std::max(static_cast<int>(upload_limit * 0.7), 2 * static_cast<int>(active.size()));
        for (auto const& item : active) {
            const int weight = item.tier == 0 ? 3 : (item.tier == 1 ? 2 : 1);
            item.handle.set_max_connections(std::max(10, available_connections * weight / std::max(1, total_weight)));
            item.handle.set_max_uploads(std::max(2, available_uploads * weight / std::max(1, total_weight)));
            // Politica "lenti": chi trasferisce ha priorità alta, chi ha fonti ma
            // è appena partito sta in mezzo, chi è fermo da troppo tempo viene
            // declassato. La priorità conta solo in contesa (c'è coda): senza
            // coda anche un declassato resta attivo e può ripartire.
            const int priority = item.tier == 0 ? 8 : (item.demoted ? 0 : (item.tier == 1 ? 6 : 3));
            item.handle.set_priority(priority);
            // Non imporre un tetto per-torrent basato sul numero di download
            // attivi: con molti torrent che scambiano pochi KB/s il pool globale
            // veniva diviso fra tutti, strozzando i torrent veloci e lasciando
            // inutilizzata parte della banda. Il tetto globale della sessione
            // (download_rate_limit) limita già il totale, quindi il singolo
            // torrent resta illimitato (0). Questo azzera anche eventuali limiti
            // per-torrent installati dalla versione precedente.
            item.handle.set_download_limit(0);
        }
        for (auto const& handle : session->session.get_torrents()) {
            auto status = handle.status();
            if (status.is_seeding || status.is_finished) handle.set_max_uploads(priority_download ? 2 : -1);
        }
    }

    // ETA-based queue ordering with a stability bias (port of extto's
    // _manage_queue ordering). Torrents are ranked by tier (0 downloading,
    // 1 with sources, 2 unknown) and then by estimated time to completion;
    // already-active torrents get a 30% bonus so a marginally better queued
    // torrent does not displace them. The order is applied only when it
    // changes, so queue positions do not thrash every pass.
    {
        struct order_item { std::string hash; int tier; double score; };
        std::vector<order_item> order;
        for (auto const& handle : session->session.get_torrents()) {
            auto status = handle.status();
            if (!status.has_metadata || status.is_seeding || status.is_finished) continue;
            const int sources = status.num_peers + status.num_seeds;
            const int tier = status.download_rate > 0 ? 0 : (sources > 0 ? 1 : 2);
            const double remaining = static_cast<double>(std::max<std::int64_t>(0, status.total_wanted - status.total_wanted_done));
            double score = status.download_rate > 0
                ? remaining / std::max(1, status.download_rate)
                : (sources > 0 ? remaining / (1 + sources) : remaining);
            const bool paused = static_cast<bool>(status.flags & lt::torrent_flags::paused);
            if (!paused) score *= 0.70; // stability bonus for already-active torrents
            order.push_back({hex_hash(handle), tier, score});
        }
        std::sort(order.begin(), order.end(), [](order_item const& a, order_item const& b) {
            if (a.tier != b.tier) return a.tier < b.tier;
            return a.score < b.score;
        });
        std::vector<std::string> new_order;
        new_order.reserve(order.size());
        for (auto const& item : order) new_order.push_back(item.hash);
        if (new_order != session->last_queue_order) {
            session->last_queue_order = new_order;
            // Apply worst-first so the best torrent ends up at the top.
            for (auto it = order.rbegin(); it != order.rend(); ++it) {
                auto handle = find_torrent(session, it->hash.c_str());
                if (handle.is_valid()) {
                    try { handle.queue_position_top(); } catch (...) {}
                }
            }
        }
    }

    // Tracking dei "lenti": dimentica i torrent usciti o finiti, così la mappa
    // non cresce nel tempo.
    for (auto it = session->download_stalled_since.begin();
         it != session->download_stalled_since.end();) {
        const bool live = std::any_of(active.begin(), active.end(), [&](const active_torrent& item) {
            return hex_hash(item.handle) == it->first;
        });
        if (live) {
            ++it;
        } else {
            it = session->download_stalled_since.erase(it);
        }
    }
}

size_t gextto_lt_peers(gextto_lt_session* session, const char* hash, gextto_lt_peer* output, size_t capacity, char* error, size_t error_size) {
    std::lock_guard<std::recursive_mutex> lock(LIBTORRENT_API_MUTEX);
    try {
        auto handle = find_torrent(session, hash);
        if (!handle.is_valid()) { set_error(error, error_size, "torrent not found"); return 0; }
        std::vector<lt::peer_info> peers;
        handle.get_peer_info(peers);
        if (output == nullptr || capacity == 0) return peers.size();
        const size_t count = std::min(capacity, peers.size());
        for (size_t i = 0; i < count; ++i) {
            output[i] = {};
            copy_string(output[i].address, sizeof(output[i].address), peers[i].ip.address().to_string() + ":" + std::to_string(peers[i].ip.port()));
            copy_string(output[i].client, sizeof(output[i].client), peers[i].client);
            output[i].download_rate = peers[i].payload_down_speed;
            output[i].upload_rate = peers[i].payload_up_speed;
            output[i].num_pieces = peers[i].num_pieces;
            output[i].seed = static_cast<bool>(peers[i].flags & lt::peer_info::seed) ? 1 : 0;
            int flags = 0;
            if (peers[i].flags & lt::peer_info::seed) flags |= 1;
            if (peers[i].source & lt::peer_info::incoming) flags |= 2;
            if (peers[i].flags & (lt::peer_info::rc4_encrypted | lt::peer_info::plaintext_encrypted)) flags |= 4;
            if (peers[i].flags & lt::peer_info::utp_socket) flags |= 8;
            output[i].flags = flags;
            output[i].progress = peers[i].progress * 100.0f;
            output[i].total_upload = peers[i].total_upload;
            output[i].total_download = peers[i].total_download;
        }
        return count;
    } catch (const std::exception& exception) {
        set_error(error, error_size, exception.what());
    }
    return 0;
}

size_t gextto_lt_trackers(gextto_lt_session* session, const char* hash, gextto_lt_tracker* output, size_t capacity, char* error, size_t error_size) {
    std::lock_guard<std::recursive_mutex> lock(LIBTORRENT_API_MUTEX);
    try {
        auto handle = find_torrent(session, hash);
        if (!handle.is_valid()) { set_error(error, error_size, "torrent not found"); return 0; }
        auto trackers = handle.trackers();
        if (output == nullptr || capacity == 0) return trackers.size();
        const size_t count = std::min(capacity, trackers.size());
        for (size_t i = 0; i < count; ++i) {
            output[i] = {};
            copy_string(output[i].url, sizeof(output[i].url), trackers[i].url);
            output[i].tier = trackers[i].tier;
            output[i].verified = trackers[i].verified ? 1 : 0;
            // Per-endpoint state lives in `endpoints` in libtorrent 2.0; the
            // announce_entry fields are deprecated under ABI v2.
            if (!trackers[i].endpoints.empty()) {
                const auto& endpoint = trackers[i].endpoints.front();
                copy_string(output[i].message, sizeof(output[i].message), endpoint.message);
                output[i].fails = endpoint.fails;
                output[i].scrape_incomplete = endpoint.scrape_incomplete;
                output[i].scrape_complete = endpoint.scrape_complete;
                output[i].scrape_downloaded = endpoint.scrape_downloaded;
            }
        }
        return count;
    } catch (const std::exception& exception) {
        set_error(error, error_size, exception.what());
    }
    return 0;
}

size_t gextto_lt_files(gextto_lt_session* session, const char* hash, gextto_lt_file* output, size_t capacity, char* error, size_t error_size) {
    std::lock_guard<std::recursive_mutex> lock(LIBTORRENT_API_MUTEX);
    try {
        auto handle = find_torrent(session, hash);
        if (!handle.is_valid()) { set_error(error, error_size, "torrent not found"); return 0; }
        auto info = handle.torrent_file();
        if (!info) return 0;
        auto storage = info->files();
        std::vector<std::int64_t> progress = handle.file_progress();
        std::vector<int> priorities = handle.file_priorities();
        const int total = storage.num_files();
        if (output == nullptr || capacity == 0) return static_cast<size_t>(total);
        const size_t count = std::min(capacity, static_cast<size_t>(total));
        for (size_t i = 0; i < count; ++i) {
            output[i] = {};
            copy_string(output[i].path, sizeof(output[i].path), storage.file_path(static_cast<int>(i)));
            output[i].size = storage.file_size(static_cast<int>(i));
            output[i].downloaded = i < progress.size() ? progress[i] : 0;
            output[i].priority = i < priorities.size()
                ? priorities[i]
                : static_cast<int>(lt::default_priority);
        }
        return count;
    } catch (const std::exception& exception) {
        set_error(error, error_size, exception.what());
    }
    return 0;
}

int gextto_lt_move_storage(gextto_lt_session* session, const char* hash, const char* destination, char* error, size_t error_size) {
    std::lock_guard<std::recursive_mutex> lock(LIBTORRENT_API_MUTEX);
    if (session == nullptr || hash == nullptr || destination == nullptr) {
        set_error(error, error_size, "invalid move storage parameters");
        return 0;
    }
    try {
        auto handle = find_torrent(session, hash);
        if (!handle.is_valid()) { set_error(error, error_size, "torrent not found"); return 0; }
        handle.move_storage(destination, lt::move_flags_t::fail_if_exist);
        return 1;
    } catch (const std::exception& exception) {
        set_error(error, error_size, exception.what());
    }
    return 0;
}

int gextto_lt_moving_storage(gextto_lt_session* session, char* names, size_t names_size, char* error, size_t error_size) {
    std::lock_guard<std::recursive_mutex> lock(LIBTORRENT_API_MUTEX);
    if (session == nullptr || names == nullptr || names_size == 0) {
        set_error(error, error_size, "invalid moving storage parameters");
        return -1;
    }
    names[0] = '\0';
    try {
        int count = 0;
        std::string joined;
        for (auto const& handle : session->session.get_torrents()) {
            if (!handle.is_valid()) continue;
            auto const status = handle.status(lt::torrent_handle::query_name);
            if (!status.moving_storage) continue;
            ++count;
            if (!joined.empty()) joined += '\n';
            joined += hex_hash(handle) + "\t" + status.name;
        }
        std::snprintf(names, names_size, "%s", joined.c_str());
        return count;
    } catch (const std::exception& exception) {
        set_error(error, error_size, exception.what());
    } catch (...) {
        set_error(error, error_size, "unknown moving storage error");
    }
    return -1;
}

int gextto_lt_associate_storage(gextto_lt_session* session, const char* hash, const char* destination, char* error, size_t error_size) {
    std::lock_guard<std::recursive_mutex> lock(LIBTORRENT_API_MUTEX);
    if (session == nullptr || hash == nullptr || destination == nullptr) {
        set_error(error, error_size, "invalid associate storage parameters");
        return 0;
    }
    try {
        auto handle = find_torrent(session, hash);
        if (!handle.is_valid()) { set_error(error, error_size, "torrent not found"); return 0; }
        // `reset_save_path` changes the save path without moving files and makes
        // libtorrent re-check the data at the new location. When the destination
        // already holds the payload this associates (and seeds) it instead of
        // failing with "already exists".
#if LIBTORRENT_VERSION_NUM >= 20007
        handle.move_storage(destination, lt::move_flags_t::reset_save_path);
        handle.force_recheck();
        return 1;
#else
        // The release archive is built on an older baseline (Ubuntu 22.04,
        // libtorrent 2.0.5) so it runs on every supported distribution; that
        // libtorrent cannot change the save path without moving the files.
        (void)handle;
        set_error(error, error_size, "associating existing data requires libtorrent 2.0.7 or newer");
        return 0;
#endif
    } catch (const std::exception& exception) {
        set_error(error, error_size, exception.what());
    } catch (...) {
        set_error(error, error_size, "unknown associate storage error");
    }
    return 0;
}

int gextto_lt_set_paused(gextto_lt_session* session, const char* hash, int paused, char* error, size_t error_size) {
    std::lock_guard<std::recursive_mutex> lock(LIBTORRENT_API_MUTEX);
    try {
        auto handle = find_torrent(session, hash);
        if (!handle.is_valid()) { set_error(error, error_size, "torrent not found"); return 0; }
        if (paused) {
            handle.unset_flags(lt::torrent_flags::auto_managed);
            handle.pause();
        } else {
            handle.set_flags(lt::torrent_flags::auto_managed);
            handle.resume();
        }
        return 1;
    } catch (const std::exception& exception) {
        set_error(error, error_size, exception.what());
    }
    return 0;
}

int gextto_lt_set_pin(gextto_lt_session* session, const char* hash, int pinned, char* error, size_t error_size) {
    std::lock_guard<std::recursive_mutex> lock(LIBTORRENT_API_MUTEX);
    try {
        auto handle = find_torrent(session, hash);
        if (pinned && !handle.is_valid()) { set_error(error, error_size, "torrent not found"); return 0; }
        const auto previous_hash = session->pinned_hash;
        const auto requested_hash = (pinned && hash != nullptr) ? std::string(hash) : std::string();
        if (!previous_hash.empty() && previous_hash != requested_hash) {
            auto previous = find_torrent(session, previous_hash.c_str());
            if (previous.is_valid()) {
                const auto status = previous.status();
                if (!(status.flags & lt::torrent_flags::paused)) {
                    previous.set_flags(lt::torrent_flags::auto_managed);
                }
            }
        }
        if (pinned) {
            handle.unset_flags(lt::torrent_flags::auto_managed);
            handle.resume();
            handle.queue_position_top();
        } else if (handle.is_valid()) {
            handle.set_flags(lt::torrent_flags::auto_managed);
            handle.resume();
        }
        session->pinned_hash = requested_hash;
        return 1;
    } catch (const std::exception& exception) {
        set_error(error, error_size, exception.what());
    } catch (...) {
        set_error(error, error_size, "unknown libtorrent pin error");
    }
    return 0;
}

int gextto_lt_set_sequential(gextto_lt_session* session, int enabled, char* error, size_t error_size) {
    std::lock_guard<std::recursive_mutex> lock(LIBTORRENT_API_MUTEX);
    try {
        if (session == nullptr) { set_error(error, error_size, "invalid session"); return 0; }
        session->sequential_enabled = enabled != 0;
        for (auto const& handle : session->session.get_torrents()) {
            if (session->sequential_enabled) {
                handle.set_flags(lt::torrent_flags::sequential_download);
            } else {
                handle.unset_flags(lt::torrent_flags::sequential_download);
            }
        }
        return 1;
    } catch (const std::exception& exception) {
        set_error(error, error_size, exception.what());
    } catch (...) {
        set_error(error, error_size, "unknown libtorrent sequential error");
    }
    return 0;
}

int gextto_lt_set_torrent_sequential(gextto_lt_session* session, const char* hash, int enabled, char* error, size_t error_size) {
    std::lock_guard<std::recursive_mutex> lock(LIBTORRENT_API_MUTEX);
    try {
        auto handle = find_torrent(session, hash);
        if (!handle.is_valid()) { set_error(error, error_size, "torrent not found"); return 0; }
        if (enabled) {
            handle.set_flags(lt::torrent_flags::sequential_download);
        } else {
            handle.unset_flags(lt::torrent_flags::sequential_download);
        }
        return 1;
    } catch (const std::exception& exception) {
        set_error(error, error_size, exception.what());
    } catch (...) {
        set_error(error, error_size, "unknown libtorrent sequential error");
    }
    return 0;
}

int gextto_lt_queue_top(gextto_lt_session* session, const char* hash, char* error, size_t error_size) {
    std::lock_guard<std::recursive_mutex> lock(LIBTORRENT_API_MUTEX);
    try {
        auto handle = find_torrent(session, hash);
        if (!handle.is_valid()) { set_error(error, error_size, "torrent not found"); return 0; }
        handle.queue_position_top();
        return 1;
    } catch (const std::exception& exception) {
        set_error(error, error_size, exception.what());
    } catch (...) {
        set_error(error, error_size, "unknown libtorrent queue error");
    }
    return 0;
}

// Prioritise the first and last ~1% of every file so playback can start early
// (qBittorrent's "first/last piece priority"). `enabled=0` restores the default
// priority for every piece.
int gextto_lt_set_first_last(gextto_lt_session* session, const char* hash, int enabled, char* error, size_t error_size) {
    std::lock_guard<std::recursive_mutex> lock(LIBTORRENT_API_MUTEX);
    try {
        auto handle = find_torrent(session, hash);
        if (!handle.is_valid()) { set_error(error, error_size, "torrent not found"); return 0; }
        auto info = handle.torrent_file();
        if (!info) { set_error(error, error_size, "torrent metadata not available"); return 0; }
        const lt::file_storage& files = info->files();
        const int piece_length = files.piece_length();
        const int num_pieces = files.num_pieces();
        if (num_pieces <= 0 || piece_length <= 0) { set_error(error, error_size, "torrent has no pieces"); return 0; }
        std::vector<lt::download_priority_t> priorities(
            static_cast<size_t>(num_pieces), lt::default_priority);
        if (enabled) {
            for (lt::file_index_t file(0); file < files.end_file(); ++file) {
                const std::int64_t size = files.file_size(file);
                if (size <= 0) continue;
                const std::int64_t offset = files.file_offset(file);
                int first = static_cast<int>(offset / piece_length);
                int last = static_cast<int>((offset + size - 1) / piece_length);
                first = std::max(0, std::min(first, num_pieces - 1));
                last = std::max(0, std::min(last, num_pieces - 1));
                const int span = last - first + 1;
                const int edge = std::max(1, span / 100);
                for (int step = 0; step < edge; ++step) {
                    if (first + step <= last) {
                        priorities[static_cast<size_t>(first + step)] = lt::top_priority;
                    }
                    if (last - step >= first) {
                        priorities[static_cast<size_t>(last - step)] = lt::top_priority;
                    }
                }
            }
        }
        handle.prioritize_pieces(priorities);
        return 1;
    } catch (const std::exception& exception) {
        set_error(error, error_size, exception.what());
    } catch (...) {
        set_error(error, error_size, "unknown libtorrent first/last error");
    }
    return 0;
}

// Splits a newline-separated payload into non-empty lines.
static std::vector<std::string> gextto_split_lines(const char* text) {
    std::vector<std::string> out;
    if (text == nullptr) return out;
    std::istringstream stream{std::string(text)};
    std::string line;
    while (std::getline(stream, line)) {
        if (!line.empty() && line.back() == '\r') line.pop_back();
        if (!line.empty()) out.push_back(line);
    }
    return out;
}

int gextto_lt_set_file_priorities(gextto_lt_session* session, const char* hash, const int* priorities, size_t count, char* error, size_t error_size) {
    std::lock_guard<std::recursive_mutex> lock(LIBTORRENT_API_MUTEX);
    try {
        auto handle = find_torrent(session, hash);
        if (!handle.is_valid()) { set_error(error, error_size, "torrent not found"); return 0; }
        auto info = handle.torrent_file();
        if (!info) { set_error(error, error_size, "torrent metadata not available"); return 0; }
        if (priorities == nullptr || count != static_cast<size_t>(info->files().num_files())) {
            set_error(error, error_size, "priority count does not match the file count");
            return 0;
        }
        std::vector<lt::download_priority_t> values;
        values.reserve(count);
        for (size_t i = 0; i < count; ++i) {
            int level = priorities[i];
            if (level < 0) level = 0;
            if (level > 7) level = 7;
            values.push_back(lt::download_priority_t(static_cast<std::uint8_t>(level)));
        }
        handle.prioritize_files(values);
        return 1;
    } catch (const std::exception& exception) {
        set_error(error, error_size, exception.what());
    } catch (...) {
        set_error(error, error_size, "unknown libtorrent file priority error");
    }
    return 0;
}

int gextto_lt_add_web_seeds(gextto_lt_session* session, const char* hash, const char* urls, int remove, char* error, size_t error_size) {
    std::lock_guard<std::recursive_mutex> lock(LIBTORRENT_API_MUTEX);
    try {
        auto handle = find_torrent(session, hash);
        if (!handle.is_valid()) { set_error(error, error_size, "torrent not found"); return 0; }
        for (const auto& url : gextto_split_lines(urls)) {
            if (remove) {
                handle.remove_url_seed(url);
            } else {
                handle.add_url_seed(url);
            }
        }
        return 1;
    } catch (const std::exception& exception) {
        set_error(error, error_size, exception.what());
    } catch (...) {
        set_error(error, error_size, "unknown libtorrent web seed error");
    }
    return 0;
}

int gextto_lt_set_trackers(gextto_lt_session* session, const char* hash, const char* tiered, char* error, size_t error_size) {
    std::lock_guard<std::recursive_mutex> lock(LIBTORRENT_API_MUTEX);
    try {
        auto handle = find_torrent(session, hash);
        if (!handle.is_valid()) { set_error(error, error_size, "torrent not found"); return 0; }
        std::vector<lt::announce_entry> entries;
        for (const auto& line : gextto_split_lines(tiered)) {
            std::string url = line;
            int tier = 0;
            auto separator = line.find('|');
            if (separator != std::string::npos) {
                tier = std::atoi(line.substr(0, separator).c_str());
                url = line.substr(separator + 1);
            }
            if (url.empty()) continue;
            lt::announce_entry entry(url);
            entry.tier = static_cast<std::uint8_t>(std::max(0, std::min(tier, 255)));
            entries.push_back(entry);
        }
        handle.replace_trackers(entries);
        return 1;
    } catch (const std::exception& exception) {
        set_error(error, error_size, exception.what());
    } catch (...) {
        set_error(error, error_size, "unknown libtorrent tracker error");
    }
    return 0;
}

int gextto_lt_set_super_seeding(gextto_lt_session* session, const char* hash, int enabled, char* error, size_t error_size) {
    std::lock_guard<std::recursive_mutex> lock(LIBTORRENT_API_MUTEX);
    try {
        auto handle = find_torrent(session, hash);
        if (!handle.is_valid()) { set_error(error, error_size, "torrent not found"); return 0; }
        if (enabled) {
            handle.set_flags(lt::torrent_flags::super_seeding);
        } else {
            handle.unset_flags(lt::torrent_flags::super_seeding);
        }
        return 1;
    } catch (const std::exception& exception) {
        set_error(error, error_size, exception.what());
    } catch (...) {
        set_error(error, error_size, "unknown libtorrent super seeding error");
    }
    return 0;
}

int gextto_lt_remove(gextto_lt_session* session, const char* hash, int delete_files, char* error, size_t error_size) {
    std::lock_guard<std::recursive_mutex> lock(LIBTORRENT_API_MUTEX);
    try {
        auto handle = find_torrent(session, hash);
        if (!handle.is_valid()) { set_error(error, error_size, "torrent not found"); return 0; }
        session->session.remove_torrent(handle, delete_files ? lt::session_handle::delete_files : lt::remove_flags_t{});
        return 1;
    } catch (const std::exception& exception) {
        set_error(error, error_size, exception.what());
    }
    return 0;
}

int gextto_lt_force_recheck(gextto_lt_session* session, const char* hash, char* error, size_t error_size) {
    std::lock_guard<std::recursive_mutex> lock(LIBTORRENT_API_MUTEX);
    try {
        auto handle = find_torrent(session, hash);
        if (!handle.is_valid()) { set_error(error, error_size, "torrent not found"); return 0; }
        handle.force_recheck();
        return 1;
    } catch (const std::exception& exception) {
        set_error(error, error_size, exception.what());
    }
    return 0;
}

int gextto_lt_reannounce(gextto_lt_session* session, const char* hash, char* error, size_t error_size) {
    std::lock_guard<std::recursive_mutex> lock(LIBTORRENT_API_MUTEX);
    try {
        auto handle = find_torrent(session, hash);
        if (!handle.is_valid()) { set_error(error, error_size, "torrent not found"); return 0; }
        handle.force_reannounce();
        return 1;
    } catch (const std::exception& exception) {
        set_error(error, error_size, exception.what());
    }
    return 0;
}

int gextto_lt_set_limits(gextto_lt_session* session, const char* hash, int download_limit, int upload_limit, char* error, size_t error_size) {
    std::lock_guard<std::recursive_mutex> lock(LIBTORRENT_API_MUTEX);
    try {
        auto handle = find_torrent(session, hash);
        if (!handle.is_valid()) { set_error(error, error_size, "torrent not found"); return 0; }
        handle.set_download_limit(download_limit);
        handle.set_upload_limit(upload_limit);
        return 1;
    } catch (const std::exception& exception) {
        set_error(error, error_size, exception.what());
    }
    return 0;
}

int gextto_lt_request_resume_save(gextto_lt_session* session, const char* state_dir) {
    std::lock_guard<std::recursive_mutex> lock(LIBTORRENT_API_MUTEX);
    if (session == nullptr || state_dir == nullptr) return -1;
    try {
        std::filesystem::create_directories(std::filesystem::path(state_dir));
        session->periodic_resume_dir = state_dir;
        int requested = 0;
        for (const auto& handle : session->session.get_torrents()) {
            if (!handle.is_valid() || !handle.need_save_resume_data()) continue;
            // Include the info dictionary: a torrent added from a .torrent file
            // has no other copy of its metadata in the state directory.
            handle.save_resume_data(lt::torrent_handle::save_info_dict);
            ++requested;
        }
        return requested;
    } catch (...) {
        return -1;
    }
}

size_t gextto_lt_restore(gextto_lt_session* session, const char* state_dir, char* error, size_t error_size) {
    std::lock_guard<std::recursive_mutex> lock(LIBTORRENT_API_MUTEX);
    if (session == nullptr || state_dir == nullptr) { set_error(error, error_size, "invalid resume restore parameters"); return 0; }
    try {
        const std::filesystem::path directory(state_dir);
        if (!std::filesystem::is_directory(directory)) return 0;
        size_t restored = 0;
        std::string warning;
        for (const auto& entry : std::filesystem::directory_iterator(directory)) {
            if (!entry.is_regular_file() || entry.path().extension() != ".fastresume") continue;
            std::ifstream input(entry.path(), std::ios::binary);
            std::vector<char> content((std::istreambuf_iterator<char>(input)), std::istreambuf_iterator<char>());
            if (content.empty()) { warning = "empty fastresume: " + entry.path().string(); continue; }
            lt::error_code ec;
            auto params = lt::read_resume_data(lt::span<char const>(content.data(), content.size()), ec);
            if (ec) { warning = "invalid fastresume " + entry.path().string() + ": " + ec.message(); continue; }
            // Un torrent che stava facendo seed prima dello shutdown viene
            // ripristinato in seed_mode: libtorrent si fida del resume e NON
            // rifà il check dei pezzi. Necessario perché il file è spesso sul
            // NAS, fuori dal path del torrent: un check lo vedrebbe mancante e
            // ripartirebbe da 0 (banda sprecata su contenuto già archiviato).
            {
                bool was_seeding = false;
                try {
                    lt::error_code bec;
                    const auto root = lt::bdecode(lt::span<char const>(content.data(), content.size()), bec);
                    if (!bec && root.type() == lt::bdecode_node::dict_t) {
                        const auto seeding_time = root.dict_find_int_value("seeding_time", 0);
                        const auto finished = root.dict_find_int_value("finished_time", 0);
                        const auto uploaded = root.dict_find_int_value("total_uploaded", 0);
                        const auto downloaded = root.dict_find_int_value("total_downloaded", 0);
                        was_seeding = seeding_time > 0
                            || (finished > 0 && uploaded > 0 && downloaded > 0
                                && uploaded >= downloaded / 2);
                    }
                } catch (...) {}
                if (was_seeding) {
                    params.flags |= lt::torrent_flags::seed_mode;
                    params.flags |= lt::torrent_flags::override_resume_data;
                }
            }
            // A magnet's fastresume may not contain enough metadata after an
            // interrupted first run.  Reuse the .torrent captured when the
            // metadata alert arrived, matching legacy's restart behaviour.
            const auto torrent_path = entry.path().parent_path() / (entry.path().stem().string() + ".torrent");
            if (std::filesystem::is_regular_file(torrent_path)) {
                lt::error_code torrent_ec;
                auto info = std::make_shared<lt::torrent_info>(torrent_path.string(), torrent_ec);
                if (!torrent_ec) params.ti = std::move(info);
            }
            session->session.add_torrent(std::move(params), ec);
            if (ec) { warning = "cannot restore " + entry.path().string() + ": " + ec.message(); continue; }
            ++restored;
        }
        if (!warning.empty()) set_error(error, error_size, warning);
        return restored;
    } catch (const std::exception& exception) {
        set_error(error, error_size, exception.what());
    }
    return 0;
}

int gextto_lt_save_resume(gextto_lt_session* session, const char* state_dir, char* error, size_t error_size) {
    std::lock_guard<std::recursive_mutex> lock(LIBTORRENT_API_MUTEX);
    if (session == nullptr || state_dir == nullptr) { set_error(error, error_size, "invalid resume save parameters"); return 0; }
    try {
        const std::filesystem::path directory(state_dir);
        std::filesystem::create_directories(directory);
        std::unordered_set<std::string> pending;
        std::unordered_set<std::string> present;
        for (const auto& handle : session->session.get_torrents()) {
            if (!handle.is_valid()) continue;
            const auto hash = hex_hash(handle);
            pending.insert(hash);
            present.insert(hash);
            // NOTE: do not request flush_disk_cache here: on shutdown it makes
            // libtorrent allocate/flush the whole disk cache and can fail with
            // std::bad_alloc for large sessions. The regular shutdown flush is
            // enough to persist progress.
            // Flush the disk cache when the session is small enough to afford
            // it: without it the saved bitfield can be ahead of the bytes
            // actually written, so a restart re-checks and loses progress. The
            // original avoided the flush for very large sessions to prevent
            // std::bad_alloc during shutdown.
            auto resume_flags = lt::torrent_handle::save_info_dict;
            if (session->session.get_torrents().size() <= 100) {
                resume_flags |= lt::torrent_handle::flush_disk_cache;
            }
            handle.save_resume_data(resume_flags);
        }
        // Rimuove i resume dei torrent non più in sessione: senza questa pulizia
        // un torrent rimosso (o fallito) verrebbe ripristinato al riavvio.
        std::error_code iterate_ec;
        for (const auto& entry : std::filesystem::directory_iterator(directory, iterate_ec)) {
            if (iterate_ec) break;
            if (!entry.is_regular_file()) continue;
            const auto extension = entry.path().extension();
            if (extension != ".fastresume" && extension != ".torrent") continue;
            if (present.find(entry.path().stem().string()) != present.end()) continue;
            std::error_code remove_ec;
            std::filesystem::remove(entry.path(), remove_ec);
        }
        const auto deadline = std::chrono::steady_clock::now() + std::chrono::seconds(30);
        std::string failure;
        while (!pending.empty() && std::chrono::steady_clock::now() < deadline) {
            session->session.wait_for_alert(std::chrono::milliseconds(250));
            std::vector<lt::alert*> alerts;
            session->session.pop_alerts(&alerts);
            for (auto* alert : alerts) {
                if (const auto* saved = lt::alert_cast<lt::save_resume_data_alert>(alert)) {
                    const auto hash = hex_hash(saved->handle);
                    if (!pending.erase(hash)) continue;
                    // Per-torrent guard: a single bad handle must not abort the
                    // whole shutdown save.
                    try {
                        auto content = lt::write_resume_data_buf(saved->params);
                        std::string write_error;
                        if (!write_atomic(directory / (hash + ".fastresume"), content, write_error)) failure = "cannot save fastresume " + hash + ": " + write_error;
                    } catch (const std::exception& exception) {
                        failure = "cannot encode fastresume " + hash + ": " + exception.what();
                    }
                } else if (const auto* failed = lt::alert_cast<lt::save_resume_data_failed_alert>(alert)) {
                    pending.erase(hex_hash(failed->handle));
                    failure = "libtorrent fastresume failed: " + failed->error.message();
                }
            }
        }
        if (!pending.empty() && failure.empty()) failure = "timed out waiting for fastresume alerts";
        if (!failure.empty()) { set_error(error, error_size, failure); return 0; }
        return 1;
    } catch (const std::exception& exception) {
        set_error(error, error_size, exception.what());
    } catch (...) {
        set_error(error, error_size, "unknown fastresume error");
    }
    return 0;
}

int gextto_lt_set_max_connections(gextto_lt_session* session, const char* hash, int max_connections, char* error, size_t error_size) {
    std::lock_guard<std::recursive_mutex> lock(LIBTORRENT_API_MUTEX);
    try {
        auto handle = find_torrent(session, hash);
        if (!handle.is_valid()) { set_error(error, error_size, "torrent not found"); return 0; }
        handle.set_max_connections(max_connections < 0 ? 0 : max_connections);
        return 1;
    } catch (const std::exception& exception) { set_error(error, error_size, exception.what()); }
    catch (...) { set_error(error, error_size, "unknown max-connections error"); }
    return 0;
}

int gextto_lt_set_max_uploads(gextto_lt_session* session, const char* hash, int max_uploads, char* error, size_t error_size) {
    std::lock_guard<std::recursive_mutex> lock(LIBTORRENT_API_MUTEX);
    try {
        auto handle = find_torrent(session, hash);
        if (!handle.is_valid()) { set_error(error, error_size, "torrent not found"); return 0; }
        handle.set_max_uploads(max_uploads < 0 ? 0 : max_uploads);
        return 1;
    } catch (const std::exception& exception) { set_error(error, error_size, exception.what()); }
    catch (...) { set_error(error, error_size, "unknown max-uploads error"); }
    return 0;
}

int gextto_lt_set_upload_mode(gextto_lt_session* session, const char* hash, int enabled, char* error, size_t error_size) {
    std::lock_guard<std::recursive_mutex> lock(LIBTORRENT_API_MUTEX);
    try {
        auto handle = find_torrent(session, hash);
        if (!handle.is_valid()) { set_error(error, error_size, "torrent not found"); return 0; }
        handle.set_upload_mode(enabled != 0);
        return 1;
    } catch (const std::exception& exception) { set_error(error, error_size, exception.what()); }
    catch (...) { set_error(error, error_size, "unknown upload-mode error"); }
    return 0;
}

int gextto_lt_set_share_mode(gextto_lt_session* session, const char* hash, int enabled, char* error, size_t error_size) {
    std::lock_guard<std::recursive_mutex> lock(LIBTORRENT_API_MUTEX);
    try {
        auto handle = find_torrent(session, hash);
        if (!handle.is_valid()) { set_error(error, error_size, "torrent not found"); return 0; }
        handle.set_share_mode(enabled != 0);
        return 1;
    } catch (const std::exception& exception) { set_error(error, error_size, exception.what()); }
    catch (...) { set_error(error, error_size, "unknown share-mode error"); }
    return 0;
}

int gextto_lt_set_torrent_flag(gextto_lt_session* session, const char* hash, int flag, int enabled, char* error, size_t error_size) {
    std::lock_guard<std::recursive_mutex> lock(LIBTORRENT_API_MUTEX);
    try {
        auto handle = find_torrent(session, hash);
        if (!handle.is_valid()) { set_error(error, error_size, "torrent not found"); return 0; }
        lt::torrent_flags_t target;
        switch (flag) {
        case GEXTTO_TFLAG_APPLY_IP_FILTER: target = lt::torrent_flags::apply_ip_filter; break;
        case GEXTTO_TFLAG_DISABLE_DHT: target = lt::torrent_flags::disable_dht; break;
        case GEXTTO_TFLAG_DISABLE_PEX: target = lt::torrent_flags::disable_pex; break;
        case GEXTTO_TFLAG_DISABLE_LSD: target = lt::torrent_flags::disable_lsd; break;
        default: set_error(error, error_size, "unknown torrent flag"); return 0;
        }
        if (enabled) handle.set_flags(target); else handle.unset_flags(target);
        return 1;
    } catch (const std::exception& exception) { set_error(error, error_size, exception.what()); }
    catch (...) { set_error(error, error_size, "unknown torrent-flag error"); }
    return 0;
}

int gextto_lt_scrape_tracker(gextto_lt_session* session, const char* hash, char* error, size_t error_size) {
    std::lock_guard<std::recursive_mutex> lock(LIBTORRENT_API_MUTEX);
    try {
        auto handle = find_torrent(session, hash);
        if (!handle.is_valid()) { set_error(error, error_size, "torrent not found"); return 0; }
        handle.scrape_tracker();
        return 1;
    } catch (const std::exception& exception) { set_error(error, error_size, exception.what()); }
    catch (...) { set_error(error, error_size, "unknown scrape error"); }
    return 0;
}

int gextto_lt_force_dht_announce(gextto_lt_session* session, const char* hash, char* error, size_t error_size) {
    std::lock_guard<std::recursive_mutex> lock(LIBTORRENT_API_MUTEX);
    try {
        auto handle = find_torrent(session, hash);
        if (!handle.is_valid()) { set_error(error, error_size, "torrent not found"); return 0; }
        handle.force_dht_announce();
        return 1;
    } catch (const std::exception& exception) { set_error(error, error_size, exception.what()); }
    catch (...) { set_error(error, error_size, "unknown dht announce error"); }
    return 0;
}

int gextto_lt_session_stats(gextto_lt_session* session, char* output, size_t output_size, char* error, size_t error_size) {
    std::lock_guard<std::recursive_mutex> lock(LIBTORRENT_API_MUTEX);
    if (session == nullptr || output == nullptr || output_size == 0) { set_error(error, error_size, "invalid session stats parameters"); return 0; }
    try {
        session->last_session_stats.clear();
        session->session.post_session_stats();
        const auto deadline = std::chrono::steady_clock::now() + std::chrono::seconds(1);
        while (session->last_session_stats.empty() && std::chrono::steady_clock::now() < deadline) {
            collect_events(session);
            if (session->last_session_stats.empty())
                std::this_thread::sleep_for(std::chrono::milliseconds(20));
        }
        std::ostringstream json;
        json << "{";
        bool first = true;
        for (auto const& entry : session->last_session_stats) {
            if (!first) json << ",";
            first = false;
            json << "\"" << entry.first << "\":" << entry.second;
        }
        json << "}";
        copy_string(output, output_size, json.str());
        return session->last_session_stats.empty() ? 0 : 1;
    } catch (const std::exception& exception) { set_error(error, error_size, exception.what()); }
    catch (...) { set_error(error, error_size, "unknown session stats error"); }
    return 0;
}
}

/* Return freed glibc arena memory to the operating system. */
#include <malloc.h>
extern "C" void gextto_trim_memory(void) {
#if defined(__GLIBC__)
    malloc_trim(0);
#endif
}
