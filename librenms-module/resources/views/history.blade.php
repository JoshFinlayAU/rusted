@extends('layouts.librenmsv1')

@section('title', 'Rusted Backup History')

@section('content')
<div class="container-fluid">
    <div class="row">
        <div class="col-md-12">
            <h2><i class="fa fa-history"></i> Backup history &mdash; {{ $device }}</h2>
            <a class="btn btn-xs btn-default" href="{{ route('rusted.index') }}">&larr; Back to devices</a>

            @if (! empty($apiError))
                <div class="alert alert-warning" style="margin-top:10px">{{ $apiError }}</div>
            @endif

            <table class="table table-condensed table-striped" style="margin-top:10px">
                <thead>
                    <tr><th>Started</th><th>Status</th><th>Bytes</th><th>Commit</th><th>Detail</th></tr>
                </thead>
                <tbody>
                @forelse ($runs as $r)
                    <tr>
                        <td>{{ $r['started_at'] ?? '' }}</td>
                        <td>
                            @php $s = $r['status'] ?? ''; @endphp
                            <span class="label label-{{ $s === 'success' ? 'success' : ($s === 'failed' ? 'danger' : 'default') }}">{{ $s }}</span>
                        </td>
                        <td>{{ $r['bytes'] ?? 0 }}</td>
                        <td><code>{{ \Illuminate\Support\Str::limit($r['commit'] ?? '', 8, '') }}</code></td>
                        <td>{{ $r['message'] ?? '' }}</td>
                    </tr>
                @empty
                    <tr><td colspan="5"><em>No backup runs recorded.</em></td></tr>
                @endforelse
                </tbody>
            </table>
        </div>
    </div>
</div>
@endsection
