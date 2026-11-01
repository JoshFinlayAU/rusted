<div style="padding: 1.5em">
    <h4>Rusted API connection</h4>
    <p>
        This plugin talks to a running <code>rusted serve</code> API. Configure the
        connection in your LibreNMS <code>.env</code> file:
    </p>
    <pre>RUSTED_API_URL=http://127.0.0.1:8080
RUSTED_API_TOKEN=your-secret-token</pre>

    <table class="table table-condensed" style="max-width:600px">
        <tr>
            <th>API URL</th>
            <td><code>{{ $api_url }}</code></td>
        </tr>
        <tr>
            <th>API token</th>
            <td>
                @if ($token_set)
                    <span class="label label-success">configured</span>
                @else
                    <span class="label label-danger">not set</span>
                @endif
            </td>
        </tr>
        <tr>
            <th>Reachable</th>
            <td>
                @if ($healthy)
                    <span class="label label-success">yes</span>
                @else
                    <span class="label label-danger">no</span>
                @endif
            </td>
        </tr>
        <tr>
            <th>rusted version</th>
            <td>
                @if ($version === null)
                    <span class="text-muted">unknown (not reachable)</span>
                @elseif ($version === '')
                    <span class="label label-warning">older than 0.2.1</span>
                    <small class="text-muted">it does not report a version; this plugin needs {{ $min_version }} or newer</small>
                @elseif ($outdated)
                    <span class="label label-danger">{{ $version }}</span>
                    <small class="text-danger">this plugin needs {{ $min_version }} or newer; upgrade rusted</small>
                @else
                    <span class="label label-success">{{ $version }}</span>
                @endif
            </td>
        </tr>
    </table>
</div>
