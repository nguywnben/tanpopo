(function () {
  try {
    var key = 'tanpopo-console-theme';
    var saved = window.localStorage.getItem(key);
    var mode =
      saved === 'light' || saved === 'dark'
        ? saved
        : window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches
          ? 'dark'
          : 'light';
    var root = document.documentElement;
    root.dataset.theme = mode;
    root.dataset.astryxTheme = 'neutral';
    root.style.colorScheme = mode;
  } catch (_err) {
    // Ignore storage access errors in restricted browsing contexts.
  }
})();
