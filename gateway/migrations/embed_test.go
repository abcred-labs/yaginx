package migrations

import "testing"

func TestNames(t *testing.T) {
	names, err := Names()
	if err != nil {
		t.Fatalf("Names() error = %v", err)
	}
	want := []string{
		"00001_legacy_baseline.sql",
		"00002_gateway_core.sql",
		"00003_config_events.sql",
		"00004_certificates.sql",
		"00005_system_settings.sql",
		"00006_node_control_plane.sql",
		"00007_crypto_key_versions.sql",
		"00008_system_setting_content_type.sql",
		"00009_request_history_sequence.sql",
		"00010_contract_schema_alignment.sql",
		"00011_plaintext_runtime_material.sql",
		"00012_resource_report_time_repair.sql",
		"00013_resource_report_hourly_index.sql",
		"00014_observability_rollups.sql",
		"00015_ip_blacklist.sql",
		"00016_website_name_unique.sql",
		"00017_v2_contract_cleanup.sql",
		"00018_system_errors.sql",
		"00019_resource_catalog_indexes.sql",
		"00020_runtime_settings.sql",
		"00021_node_restart_control.sql",
		"00022_managed_web_domains.sql",
		"00023_api_credentials.sql",
		"00024_account_sessions.sql",
		"00025_node_auto_activation.sql",
		"00026_acme_order_logs.sql",
	}
	if len(names) != len(want) {
		t.Fatalf("Names() = %v, want %v", names, want)
	}
	for index := range want {
		if names[index] != want[index] {
			t.Fatalf("Names()[%d] = %q, want %q", index, names[index], want[index])
		}
	}
}
