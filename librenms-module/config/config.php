<?php

// Configuration for the rusted LibreNMS plugin.
//
// Set these in your LibreNMS .env file. The API token must match the token the
// rusted API server was started with (RUSTED_API_TOKEN / --token).
return [
    // Base URL of the rusted HTTP API (see `rusted serve`).
    'api_url' => env('RUSTED_API_URL', 'http://127.0.0.1:8080'),

    // Bearer token for the rusted API.
    'api_token' => env('RUSTED_API_TOKEN', ''),

    // Request timeout in seconds (backups can take a while).
    'timeout' => (int) env('RUSTED_API_TIMEOUT', 120),
];
