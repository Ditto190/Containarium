'use client';

import React, { useState, useEffect, useCallback, useMemo } from 'react';
import {
  Box,
  Typography,
  CircularProgress,
  Alert,
  IconButton,
  Paper,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  Chip,
  Button,
  Stack,
  Collapse,
  MenuItem,
  Select,
  FormControl,
  InputLabel,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  TextField,
  LinearProgress,
  TablePagination,
} from '@mui/material';
import RefreshIcon from '@mui/icons-material/Refresh';
import PlayArrowIcon from '@mui/icons-material/PlayArrow';
import DownloadIcon from '@mui/icons-material/Download';
import ExpandMoreIcon from '@mui/icons-material/ExpandMore';
import ExpandLessIcon from '@mui/icons-material/ExpandLess';
import VisibilityOffIcon from '@mui/icons-material/VisibilityOff';
import Tooltip from '@mui/material/Tooltip';
import Snackbar from '@mui/material/Snackbar';
import { Server } from '@/src/types/server';
import { ZapAlert, ZapAlertSummary, ZapScanRun, ZapConfig } from '@/src/types/security';
import { getClient } from '@/src/lib/api/client';

interface ZapViewProps {
  server: Server;
}

function formatDate(iso: string): string {
  if (!iso) return '-';
  try {
    return new Date(iso).toLocaleString();
  } catch {
    return iso;
  }
}

function RiskChip({ risk }: { risk: string }) {
  const colorMap: Record<string, 'error' | 'warning' | 'info' | 'default'> = {
    high: 'error',
    medium: 'warning',
    low: 'info',
    informational: 'default',
  };
  return (
    <Chip
      label={risk}
      color={colorMap[risk] || 'default'}
      size="small"
      sx={risk === 'high' ? { fontWeight: 'bold' } : undefined}
    />
  );
}

function StatusChip({ status }: { status: string }) {
  switch (status) {
    case 'open':
      return <Chip label="Open" color="error" size="small" variant="outlined" />;
    case 'resolved':
      return <Chip label="Resolved" color="success" size="small" variant="outlined" />;
    case 'suppressed':
      return <Chip label="Suppressed" size="small" variant="outlined" />;
    default:
      return <Chip label={status} size="small" />;
  }
}

function SummaryCard({ title, value, color }: { title: string; value: number; color: string }) {
  return (
    <Paper sx={{ p: 2, textAlign: 'center', minWidth: 110 }}>
      <Typography variant="h4" sx={{ color, fontWeight: 'bold' }}>
        {value}
      </Typography>
      <Typography variant="body2" color="text.secondary">
        {title}
      </Typography>
    </Paper>
  );
}

function AlertRow({ alert, onSuppress }: { alert: ZapAlert; onSuppress: (id: number) => void }) {
  const [expanded, setExpanded] = useState(false);

  return (
    <>
      <TableRow hover sx={{ cursor: 'pointer' }} onClick={() => setExpanded(!expanded)}>
        <TableCell>
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5 }}>
            {expanded ? <ExpandLessIcon fontSize="small" /> : <ExpandMoreIcon fontSize="small" />}
            <RiskChip risk={alert.risk} />
          </Box>
        </TableCell>
        <TableCell>
          <Typography variant="body2" sx={{ fontWeight: 500 }}>{alert.alertName}</Typography>
        </TableCell>
        <TableCell>
          <Typography variant="body2" sx={{ fontFamily: 'monospace', fontSize: '0.8rem', maxWidth: 300, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
            {alert.url}
          </Typography>
        </TableCell>
        <TableCell>
          <Chip label={alert.confidence} size="small" variant="outlined" />
        </TableCell>
        <TableCell><StatusChip status={alert.status} /></TableCell>
        <TableCell>{formatDate(alert.lastSeenAt)}</TableCell>
        <TableCell align="right">
          {alert.status === 'open' && (
            <Tooltip title="Suppress alert">
              <IconButton size="small" onClick={(e) => { e.stopPropagation(); onSuppress(alert.id); }}>
                <VisibilityOffIcon fontSize="small" />
              </IconButton>
            </Tooltip>
          )}
        </TableCell>
      </TableRow>
      <TableRow>
        <TableCell colSpan={7} sx={{ py: 0, borderBottom: expanded ? undefined : 'none' }}>
          <Collapse in={expanded} timeout="auto" unmountOnExit>
            <Box sx={{ py: 2, pl: 4 }}>
              <Stack spacing={1}>
                {alert.description && (
                  <Box>
                    <Typography variant="caption" color="text.secondary">Description</Typography>
                    <Typography variant="body2">{alert.description}</Typography>
                  </Box>
                )}
                {alert.evidence && (
                  <Box>
                    <Typography variant="caption" color="text.secondary">Evidence</Typography>
                    <Typography variant="body2" sx={{ fontFamily: 'monospace', fontSize: '0.8rem', bgcolor: 'grey.100', p: 1, borderRadius: 1, whiteSpace: 'pre-wrap' }}>
                      {alert.evidence}
                    </Typography>
                  </Box>
                )}
                {alert.solution && (
                  <Box>
                    <Typography variant="caption" color="text.secondary">Solution</Typography>
                    <Typography variant="body2">{alert.solution}</Typography>
                  </Box>
                )}
                {alert.cweIds && (
                  <Box>
                    <Typography variant="caption" color="text.secondary">CWE IDs</Typography>
                    <Typography variant="body2" sx={{ fontFamily: 'monospace' }}>{alert.cweIds}</Typography>
                  </Box>
                )}
                {alert.references && (
                  <Box>
                    <Typography variant="caption" color="text.secondary">References</Typography>
                    <Typography variant="body2" sx={{ fontSize: '0.8rem', whiteSpace: 'pre-wrap' }}>{alert.references}</Typography>
                  </Box>
                )}
                <Stack direction="row" spacing={2}>
                  <Typography variant="caption" color="text.secondary">Method: {alert.method || 'GET'}</Typography>
                  <Typography variant="caption" color="text.secondary">Plugin: {alert.pluginId}</Typography>
                  <Typography variant="caption" color="text.secondary">First seen: {formatDate(alert.firstSeenAt)}</Typography>
                  <Typography variant="caption" color="text.secondary">Last seen: {formatDate(alert.lastSeenAt)}</Typography>
                  {alert.resolvedAt && <Typography variant="caption" color="text.secondary">Resolved: {formatDate(alert.resolvedAt)}</Typography>}
                </Stack>
              </Stack>
            </Box>
          </Collapse>
        </TableCell>
      </TableRow>
    </>
  );
}

export default function ZapView({ server }: ZapViewProps) {
  const [summary, setSummary] = useState<ZapAlertSummary | null>(null);
  const [alerts, setAlerts] = useState<ZapAlert[]>([]);
  const [totalCount, setTotalCount] = useState(0);
  const [scanRuns, setScanRuns] = useState<ZapScanRun[]>([]);
  const [config, setConfig] = useState<ZapConfig | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [scanning, setScanning] = useState(false);
  const [snackMessage, setSnackMessage] = useState<string | null>(null);

  // Filters
  const [riskFilter, setRiskFilter] = useState('');
  const [statusFilter, setStatusFilter] = useState('open');

  // Pagination
  const [page, setPage] = useState(0);
  const [rowsPerPage, setRowsPerPage] = useState(25);

  // Suppress dialog
  const [suppressId, setSuppressId] = useState<number | null>(null);
  const [suppressReason, setSuppressReason] = useState('');

  // Show scan history
  const [showScanHistory, setShowScanHistory] = useState(false);

  // Download state
  const [downloadFormat, setDownloadFormat] = useState<'csv' | 'json'>('csv');
  const [downloading, setDownloading] = useState(false);

  // Install state
  const [installing, setInstalling] = useState(false);

  const loadData = useCallback(async () => {
    try {
      const client = getClient(server);
      const limit = rowsPerPage === -1 ? 1000 : rowsPerPage;
      const offset = rowsPerPage === -1 ? 0 : page * rowsPerPage;

      const [summaryResp, alertsResp, runsResp, configResp] = await Promise.all([
        client.getZapAlertSummary(),
        client.listZapAlerts({
          risk: riskFilter || undefined,
          status: statusFilter || undefined,
          limit,
          offset,
        }),
        client.listZapScanRuns(10),
        client.getZapConfig(),
      ]);
      setSummary(summaryResp.summary);
      setAlerts(alertsResp.alerts);
      setTotalCount(alertsResp.totalCount);
      setScanRuns(runsResp.scanRuns);
      setConfig(configResp.config);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load ZAP data');
    } finally {
      setIsLoading(false);
    }
  }, [server, riskFilter, statusFilter, page, rowsPerPage]);

  useEffect(() => {
    loadData();
    const interval = setInterval(loadData, 60000);
    return () => clearInterval(interval);
  }, [loadData]);

  const handleTriggerScan = async () => {
    setScanning(true);
    try {
      const client = getClient(server);
      const result = await client.triggerZapScan();
      setSnackMessage(result.message || `Scan started: ${result.scanRunId}`);
      const pollInterval = setInterval(async () => {
        try {
          const runsResp = await client.listZapScanRuns(5);
          setScanRuns(runsResp.scanRuns);
          const latest = runsResp.scanRuns[0];
          if (latest && latest.id === result.scanRunId && latest.status !== 'running') {
            clearInterval(pollInterval);
            setScanning(false);
            loadData();
          }
        } catch {
          // ignore polling errors
        }
      }, 10000); // ZAP scans are slow, poll every 10s
      // Safety: stop polling after 30 minutes
      setTimeout(() => { clearInterval(pollInterval); setScanning(false); }, 1800000);
    } catch (err) {
      setSnackMessage(err instanceof Error ? err.message : 'Scan trigger failed');
      setScanning(false);
    }
  };

  const handleSuppress = async () => {
    if (suppressId === null) return;
    try {
      const client = getClient(server);
      await client.suppressZapAlert(suppressId, suppressReason);
      setSnackMessage('Alert suppressed');
      setSuppressId(null);
      setSuppressReason('');
      loadData();
    } catch (err) {
      setSnackMessage(err instanceof Error ? err.message : 'Failed to suppress alert');
    }
  };

  const handleDownload = async () => {
    setDownloading(true);
    try {
      const client = getClient(server);
      const resp = await client.listZapAlerts({
        risk: riskFilter || undefined,
        status: statusFilter || undefined,
        limit: 1000,
        offset: 0,
      });
      const allAlerts = resp.alerts;
      const dateStr = new Date().toISOString().slice(0, 10);
      let blob: Blob;
      let filename: string;

      if (downloadFormat === 'json') {
        blob = new Blob([JSON.stringify(allAlerts, null, 2)], { type: 'application/json' });
        filename = `zap-alerts-${dateStr}.json`;
      } else {
        const columns = ['id', 'risk', 'confidence', 'alertName', 'description', 'url', 'method', 'evidence', 'solution', 'cweIds', 'status', 'firstSeenAt', 'lastSeenAt', 'resolvedAt', 'suppressed', 'suppressedReason'] as const;
        const escape = (v: unknown) => {
          const s = String(v ?? '');
          return s.includes(',') || s.includes('"') || s.includes('\n') ? `"${s.replace(/"/g, '""')}"` : s;
        };
        const rows = [columns.join(',')];
        for (const a of allAlerts) {
          rows.push(columns.map((col) => escape(a[col])).join(','));
        }
        blob = new Blob([rows.join('\n')], { type: 'text/csv' });
        filename = `zap-alerts-${dateStr}.csv`;
      }

      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = filename;
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);
      URL.revokeObjectURL(url);
    } catch (err) {
      setSnackMessage(err instanceof Error ? err.message : 'Download failed');
    } finally {
      setDownloading(false);
    }
  };

  const handleDownloadReport = async (scanRunId: string) => {
    try {
      const client = getClient(server);
      const report = await client.getZapReport(scanRunId, 'html');
      const blob = new Blob([report.content], { type: report.contentType || 'text/html' });
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = report.filename || `zap-report-${scanRunId}.html`;
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);
      URL.revokeObjectURL(url);
    } catch (err) {
      setSnackMessage(err instanceof Error ? err.message : 'Failed to download report');
    }
  };

  const handleInstallZap = async () => {
    setInstalling(true);
    try {
      const client = getClient(server);
      const result = await client.installZap();
      setSnackMessage(result.message || 'ZAP installed');
      if (result.success) {
        loadData();
      }
    } catch (err) {
      setSnackMessage(err instanceof Error ? err.message : 'Failed to install ZAP');
    } finally {
      setInstalling(false);
    }
  };

  if (isLoading) {
    return (
      <Box sx={{ display: 'flex', justifyContent: 'center', alignItems: 'center', height: '40vh' }}>
        <CircularProgress />
      </Box>
    );
  }

  if (error) {
    return (
      <Box sx={{ p: 3 }}>
        <Alert severity="error" action={
          <IconButton color="inherit" size="small" onClick={loadData}><RefreshIcon /></IconButton>
        }>
          {error}
        </Alert>
      </Box>
    );
  }

  return (
    <Box>
      {/* Header */}
      <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', mb: 3 }}>
        <Box>
          <Typography variant="h6">OWASP ZAP Web Application Scan</Typography>
          {config && (
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, flexWrap: 'wrap' }}>
              <Typography variant="caption" color="text.secondary">
                Interval: {config.interval}
                {config.zapAvailable && config.zapVersion && ` | ZAP: ${config.zapVersion}`}
                {config.zapAvailable && !config.zapVersion && ' | ZAP: installed'}
              </Typography>
              {!config.zapAvailable && (
                <Button
                  size="small"
                  variant="outlined"
                  startIcon={installing ? <CircularProgress size={14} /> : <DownloadIcon />}
                  onClick={handleInstallZap}
                  disabled={installing}
                  sx={{ textTransform: 'none', fontSize: '0.7rem', py: 0 }}
                >
                  Install ZAP
                </Button>
              )}
            </Box>
          )}
        </Box>
        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
          <Button
            variant="contained"
            size="small"
            startIcon={scanning ? <CircularProgress size={16} color="inherit" /> : <PlayArrowIcon />}
            onClick={handleTriggerScan}
            disabled={scanning || (config !== null && !config.zapAvailable)}
          >
            Run Scan
          </Button>
          <IconButton onClick={loadData} size="small"><RefreshIcon /></IconButton>
        </Box>
      </Box>

      {/* Scan Progress */}
      {scanRuns.length > 0 && scanRuns[0].status === 'running' && (
        <Box sx={{ mb: 3 }}>
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 0.5 }}>
            <Typography variant="body2" color="text.secondary">
              Scanning... {scanRuns[0].completedCount}/{scanRuns[0].targetsCount} targets
            </Typography>
          </Box>
          <LinearProgress
            variant="determinate"
            value={scanRuns[0].targetsCount > 0 ? (scanRuns[0].completedCount / scanRuns[0].targetsCount) * 100 : 0}
          />
        </Box>
      )}

      {/* Summary Cards */}
      {summary && (
        <Stack direction="row" spacing={2} sx={{ mb: 3, flexWrap: 'wrap' }}>
          <SummaryCard title="Open" value={summary.openAlerts} color="error.main" />
          <SummaryCard title="High" value={summary.highCount} color="#d32f2f" />
          <SummaryCard title="Medium" value={summary.mediumCount} color="warning.main" />
          <SummaryCard title="Low" value={summary.lowCount} color="info.main" />
          <SummaryCard title="Info" value={summary.infoCount} color="text.secondary" />
          <SummaryCard title="Resolved" value={summary.resolvedAlerts} color="success.main" />
          <SummaryCard title="Suppressed" value={summary.suppressedAlerts} color="text.disabled" />
        </Stack>
      )}

      {/* Filters */}
      <Paper sx={{ p: 2, mb: 3 }}>
        <Stack direction="row" spacing={2} alignItems="center">
          <FormControl size="small" sx={{ minWidth: 140 }}>
            <InputLabel>Risk Level</InputLabel>
            <Select value={riskFilter} label="Risk Level" onChange={(e) => { setRiskFilter(e.target.value); setPage(0); }}>
              <MenuItem value="">All</MenuItem>
              <MenuItem value="high">High</MenuItem>
              <MenuItem value="medium">Medium</MenuItem>
              <MenuItem value="low">Low</MenuItem>
              <MenuItem value="informational">Informational</MenuItem>
            </Select>
          </FormControl>
          <FormControl size="small" sx={{ minWidth: 120 }}>
            <InputLabel>Status</InputLabel>
            <Select value={statusFilter} label="Status" onChange={(e) => { setStatusFilter(e.target.value); setPage(0); }}>
              <MenuItem value="">All</MenuItem>
              <MenuItem value="open">Open</MenuItem>
              <MenuItem value="resolved">Resolved</MenuItem>
              <MenuItem value="suppressed">Suppressed</MenuItem>
            </Select>
          </FormControl>
          <Typography variant="body2" color="text.secondary">
            {totalCount} alerts
          </Typography>
          <Box sx={{ ml: 'auto', display: 'flex', alignItems: 'center', gap: 1 }}>
            <Select
              size="small"
              value={downloadFormat}
              onChange={(e) => setDownloadFormat(e.target.value as 'csv' | 'json')}
              sx={{ minWidth: 80, height: 32 }}
            >
              <MenuItem value="csv">CSV</MenuItem>
              <MenuItem value="json">JSON</MenuItem>
            </Select>
            <Button
              size="small"
              variant="outlined"
              startIcon={downloading ? <CircularProgress size={14} /> : <DownloadIcon />}
              onClick={handleDownload}
              disabled={downloading || totalCount === 0}
            >
              Download
            </Button>
          </Box>
        </Stack>
      </Paper>

      {/* Alerts Table */}
      <TableContainer component={Paper} sx={{ mb: 3 }}>
        <Table size="small">
          <TableHead>
            <TableRow>
              <TableCell sx={{ width: 100 }}>Risk</TableCell>
              <TableCell>Alert</TableCell>
              <TableCell>URL</TableCell>
              <TableCell sx={{ width: 100 }}>Confidence</TableCell>
              <TableCell sx={{ width: 100 }}>Status</TableCell>
              <TableCell sx={{ width: 160 }}>Last Seen</TableCell>
              <TableCell align="right" sx={{ width: 60 }}>Actions</TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {alerts.length === 0 ? (
              <TableRow>
                <TableCell colSpan={7} align="center">
                  <Typography color="text.secondary" sx={{ py: 4 }}>
                    {statusFilter === 'open' ? 'No open ZAP alerts.' : 'No alerts match the current filters.'}
                  </Typography>
                </TableCell>
              </TableRow>
            ) : (
              alerts.map((alert) => (
                <AlertRow key={alert.id} alert={alert} onSuppress={(id) => setSuppressId(id)} />
              ))
            )}
          </TableBody>
        </Table>
        {totalCount > 0 && (
          <TablePagination
            component="div"
            count={totalCount}
            page={page}
            onPageChange={(_, newPage) => setPage(newPage)}
            rowsPerPage={rowsPerPage}
            onRowsPerPageChange={(e) => { setRowsPerPage(parseInt(e.target.value, 10)); setPage(0); }}
            rowsPerPageOptions={[10, 25, 50, 100]}
          />
        )}
      </TableContainer>

      {/* Scan History Toggle */}
      <Button
        size="small"
        variant="text"
        onClick={() => setShowScanHistory(!showScanHistory)}
        startIcon={showScanHistory ? <ExpandLessIcon /> : <ExpandMoreIcon />}
        sx={{ mb: 1 }}
      >
        Scan History ({scanRuns.length})
      </Button>

      <Collapse in={showScanHistory}>
        <TableContainer component={Paper}>
          <Table size="small">
            <TableHead>
              <TableRow>
                <TableCell>Started</TableCell>
                <TableCell>Trigger</TableCell>
                <TableCell>Status</TableCell>
                <TableCell>Targets</TableCell>
                <TableCell>High</TableCell>
                <TableCell>Medium</TableCell>
                <TableCell>Low</TableCell>
                <TableCell>Duration</TableCell>
                <TableCell align="right">Report</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {scanRuns.map((run) => (
                <TableRow key={run.id}>
                  <TableCell>{formatDate(run.startedAt)}</TableCell>
                  <TableCell><Chip label={run.trigger} size="small" variant="outlined" /></TableCell>
                  <TableCell>
                    <Chip
                      label={run.status}
                      size="small"
                      color={run.status === 'completed' ? 'success' : run.status === 'failed' ? 'error' : 'info'}
                    />
                  </TableCell>
                  <TableCell>{run.targetsCount}</TableCell>
                  <TableCell>{run.highCount > 0 ? <Typography color="error" variant="body2" fontWeight="bold">{run.highCount}</Typography> : '-'}</TableCell>
                  <TableCell>{run.mediumCount > 0 ? <Typography color="warning.main" variant="body2">{run.mediumCount}</Typography> : '-'}</TableCell>
                  <TableCell>{run.lowCount > 0 ? run.lowCount : '-'}</TableCell>
                  <TableCell>{run.duration || '-'}</TableCell>
                  <TableCell align="right">
                    {run.status === 'completed' && (
                      <Tooltip title="Download HTML report">
                        <IconButton size="small" onClick={() => handleDownloadReport(run.id)}>
                          <DownloadIcon fontSize="small" />
                        </IconButton>
                      </Tooltip>
                    )}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </TableContainer>
      </Collapse>

      {/* Suppress Dialog */}
      <Dialog open={suppressId !== null} onClose={() => setSuppressId(null)}>
        <DialogTitle>Suppress ZAP Alert</DialogTitle>
        <DialogContent>
          <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
            Suppressed alerts are excluded from open counts and alerts.
          </Typography>
          <TextField
            label="Reason"
            fullWidth
            multiline
            rows={2}
            value={suppressReason}
            onChange={(e) => setSuppressReason(e.target.value)}
            placeholder="e.g., Accepted risk, false positive, handled externally"
          />
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setSuppressId(null)}>Cancel</Button>
          <Button onClick={handleSuppress} variant="contained">Suppress</Button>
        </DialogActions>
      </Dialog>

      <Snackbar
        open={!!snackMessage}
        autoHideDuration={5000}
        onClose={() => setSnackMessage(null)}
        message={snackMessage}
      />
    </Box>
  );
}
