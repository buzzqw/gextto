package gextto

// SaveLibraryConfig is the config-persistence helper used by the web handler;
// it mirrors Config::save_library without colliding with the handler name.
func SaveLibraryConfig(dataDir string, series []SeriesConfig, movies []MovieConfig) error {
	return SaveLibrary(dataDir, series, movies)
}
