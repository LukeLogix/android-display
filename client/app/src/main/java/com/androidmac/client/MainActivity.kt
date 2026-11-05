package com.androidmac.client

import android.Manifest
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Build
import android.os.Bundle
import android.util.Log
import android.view.View
import android.widget.Button
import android.widget.RadioGroup
import android.widget.TextView
import android.widget.Toast
import androidx.appcompat.app.AppCompatActivity
import androidx.core.app.ActivityCompat
import androidx.core.content.ContextCompat
import androidx.lifecycle.lifecycleScope
import com.androidmac.client.control.ControlClient
import com.androidmac.client.discovery.NsdDiscovery
import com.androidmac.client.service.DisplayService
import com.google.android.material.textfield.TextInputEditText
import com.google.android.material.textfield.TextInputLayout
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.catch
import kotlinx.coroutines.launch

class MainActivity : AppCompatActivity() {

    companion object {
        private const val TAG = "MainActivity"
        private const val DEFAULT_PORT = 9000
        private const val NOTIFICATION_PERMISSION_CODE = 1001
    }

    private lateinit var statusText: TextView
    private lateinit var manualIpInput: TextInputEditText
    private lateinit var ipInputLayout: TextInputLayout
    private lateinit var connectButton: Button
    private lateinit var connectionMode: RadioGroup

    private var discovery: NsdDiscovery? = null
    private var discoveryJob: Job? = null

    private var discoveredHost: String? = null
    private var discoveredPort: Int = DEFAULT_PORT
    private var isUsbMode: Boolean = false

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_main)

        statusText = findViewById(R.id.statusText)
        manualIpInput = findViewById(R.id.manualIpInput)
        ipInputLayout = findViewById(R.id.ipInputLayout)
        connectButton = findViewById(R.id.connectButton)
        connectionMode = findViewById(R.id.connectionMode)
        connectButton.setOnClickListener { onConnectClicked() }

        // WiFi / USB 模式切換
        connectionMode.setOnCheckedChangeListener { _, checkedId ->
            isUsbMode = (checkedId == R.id.modeUsb)
            if (isUsbMode) {
                // USB 模式：固定 localhost，隱藏 IP 輸入和搜尋狀態
                manualIpInput.setText("127.0.0.1")
                ipInputLayout.visibility = View.GONE
                statusText.text = "USB 模式 — 請確認裝置已透過 USB 連接"
                discoveryJob?.cancel()
            } else {
                // WiFi 模式：恢復搜尋
                ipInputLayout.visibility = View.VISIBLE
                manualIpInput.setText(discoveredHost ?: "")
                statusText.text = "Scanning..."
                startDiscovery()
            }
        }

        // 請求通知權限 (Android 13+)
        requestNotificationPermission()

        startDiscovery()
    }

    private fun requestNotificationPermission() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            if (ContextCompat.checkSelfPermission(this, Manifest.permission.POST_NOTIFICATIONS)
                != PackageManager.PERMISSION_GRANTED
            ) {
                ActivityCompat.requestPermissions(
                    this,
                    arrayOf(Manifest.permission.POST_NOTIFICATIONS),
                    NOTIFICATION_PERMISSION_CODE
                )
            }
        }
    }

    private fun startDiscovery() {
        discoveryJob?.cancel()
        discovery = NsdDiscovery(this)
        discoveryJob = lifecycleScope.launch {
            discovery!!.discover()
                .catch { e -> Log.e(TAG, "Discovery error: ${e.message}") }
                .collect { server ->
                    discoveredHost = server.host
                    discoveredPort = server.port
                    if (!isUsbMode) {
                        statusText.text = "Found: ${server.name} (${server.host}:${server.port})"
                        manualIpInput.setText(server.host)
                    }
                    Log.d(TAG, "Discovered server: $server")
                }
        }
    }

    private fun onConnectClicked() {
        val connectionType = if (isUsbMode) "usb" else "wifi"
        val host = if (isUsbMode) "127.0.0.1" else {
            val ip = manualIpInput.text?.toString()?.trim()
            if (ip.isNullOrEmpty()) {
                Toast.makeText(this, "Enter an IP address or wait for discovery", Toast.LENGTH_SHORT).show()
                return
            }
            ip
        }
        val port = DEFAULT_PORT

        connectButton.isEnabled = false
        statusText.text = "Connecting to $host:$port ($connectionType)..."

        lifecycleScope.launch {
            try {
                // 在 MainActivity 執行握手以便即時回饋錯誤
                val controlClient = ControlClient()
                val metrics = resources.displayMetrics

                // The Mac app decides the streamed resolution and bitrate; send the native size.
                val serverHello = controlClient.connect(
                    host, port, metrics.widthPixels, metrics.heightPixels, metrics.densityDpi, connectionType
                )

                Log.d(TAG, "Handshake complete: $serverHello")

                // 將 ControlClient 暫存給 DisplayService
                DisplayService.pendingControlClient = controlClient

                // 啟動前景服務
                val serviceIntent = Intent(this@MainActivity, DisplayService::class.java).apply {
                    putExtra("serverHost", host)
                    putExtra("touchPort", serverHello.touchPort)
                    putExtra("videoPort", serverHello.videoPort)
                    putExtra("width", serverHello.virtualDisplay.width)
                    putExtra("height", serverHello.virtualDisplay.height)
                    putExtra("connectionType", connectionType)
                }
                startForegroundService(serviceIntent)

                // 啟動 DisplayActivity
                val displayIntent = Intent(this@MainActivity, DisplayActivity::class.java).apply {
                    putExtra("width", serverHello.virtualDisplay.width)
                    putExtra("height", serverHello.virtualDisplay.height)
                }
                startActivity(displayIntent)
            } catch (e: Exception) {
                Log.e(TAG, "Connection failed: ${e.message}", e)
                Toast.makeText(
                    this@MainActivity,
                    "Connection failed: ${e.message}",
                    Toast.LENGTH_LONG
                ).show()
                statusText.text = "Connection failed. Tap Connect to retry."
            } finally {
                connectButton.isEnabled = true
            }
        }
    }

    override fun onDestroy() {
        super.onDestroy()
        discoveryJob?.cancel()
    }
}
