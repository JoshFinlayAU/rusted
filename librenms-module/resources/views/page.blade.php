@extends('layouts.librenmsv1')

@section('title', 'Rusted Backups')

@section('content')
<div class="container-fluid">
    <div class="row">
        <div class="col-md-12">
            <h2><i class="fa fa-floppy-o"></i> Rusted &mdash; Configuration Backups</h2>

            {{-- Visual feedback --}}
            @if (session('status'))
                <div class="alert alert-success">{{ session('status') }}</div>
            @endif
            @if (session('error'))
                <div class="alert alert-danger">{{ session('error') }}</div>
            @endif
            @if (! empty($apiError))
                <div class="alert alert-warning">{{ $apiError }}</div>
            @endif
        </div>
    </div>

    <div class="row">
        <div class="col-md-12">
            <div class="panel panel-default">
                <div class="panel-heading"><strong>Devices</strong></div>
                <div class="panel-body">
                    <table class="table table-condensed table-hover">
                        <thead>
                            <tr>
                                <th>Name</th><th>Host</th><th>Port</th>
                                <th>Driver</th><th>Group</th><th>Enabled</th><th>Actions</th>
                            </tr>
                        </thead>
                        <tbody>
                        @forelse ($devices as $d)
                            <tr>
                                <td>{{ $d['name'] }}</td>
                                <td>{{ $d['host'] }}</td>
                                <td>{{ $d['port'] }}</td>
                                <td>{{ $d['driver'] }}</td>
                                <td>{{ $d['group'] ?: '-' }}</td>
                                <td>
                                    @if ($d['enabled'])
                                        <span class="label label-success">yes</span>
                                    @else
                                        <span class="label label-default">no</span>
                                    @endif
                                </td>
                                <td>
                                    <form method="POST" action="{{ route('rusted.devices.backup', ['name' => $d['name']]) }}" style="display:inline">
                                        @csrf
                                        <button class="btn btn-xs btn-primary" type="submit">
                                            <i class="fa fa-download"></i> Backup
                                        </button>
                                    </form>
                                    <a class="btn btn-xs btn-default" href="{{ route('rusted.devices.history', ['name' => $d['name']]) }}">
                                        <i class="fa fa-history"></i> History
                                    </a>
                                    <form method="POST" action="{{ route('rusted.devices.destroy', ['name' => $d['name']]) }}" style="display:inline"
                                          onsubmit="return confirm('Remove {{ $d['name'] }}?');">
                                        @csrf
                                        @method('DELETE')
                                        <button class="btn btn-xs btn-danger" type="submit">
                                            <i class="fa fa-trash"></i> Remove
                                        </button>
                                    </form>
                                </td>
                            </tr>
                        @empty
                            <tr><td colspan="7"><em>No devices yet.</em></td></tr>
                        @endforelse
                        </tbody>
                    </table>
                </div>
            </div>
        </div>
    </div>

    <div class="row">
        <div class="col-md-12">
            <div class="panel panel-default">
                <div class="panel-heading"><strong>Add device</strong></div>
                <div class="panel-body">
                    <form method="POST" action="{{ route('rusted.devices.store') }}" class="form-inline">
                        @csrf
                        <input class="form-control input-sm" type="text" name="name" placeholder="name" required>
                        <input class="form-control input-sm" type="text" name="host" placeholder="host / IP" required>
                        <input class="form-control input-sm" type="number" name="port" placeholder="22" style="width:80px">
                        <select class="form-control input-sm" name="driver" required>
                            <option value="" disabled selected>driver</option>
                            @foreach ($drivers as $drv)
                                <option value="{{ $drv['name'] }}">{{ $drv['name'] }}</option>
                            @endforeach
                        </select>
                        <input class="form-control input-sm" type="text" name="credential" placeholder="credential" required>
                        <input class="form-control input-sm" type="text" name="group" placeholder="group (optional)">
                        <button class="btn btn-sm btn-success" type="submit"><i class="fa fa-plus"></i> Add</button>
                    </form>
                    <p class="help-block" style="margin-top:8px">
                        Credentials are managed with the <code>rusted cred</code> CLI; reference an existing credential by name.
                    </p>
                </div>
            </div>
        </div>
    </div>
</div>
@endsection
