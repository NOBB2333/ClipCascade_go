package com.clipcascade.android

import android.Manifest
import android.app.Activity
import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.net.nsd.NsdManager
import android.net.nsd.NsdServiceInfo
import android.os.Build
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.provider.Settings
import android.util.Log
import android.view.ViewGroup
import android.widget.ArrayAdapter
import android.widget.Button
import android.widget.CheckBox
import android.widget.EditText
import android.widget.LinearLayout
import android.widget.ListView
import android.widget.PopupMenu
import android.widget.TextView
import android.widget.Toast
import androidx.core.app.ActivityCompat
import androidx.core.content.ContextCompat
import java.text.SimpleDateFormat
import java.util.Date
import java.util.LinkedHashSet
import java.util.Locale

class MainActivity : Activity() {

    private val tag = "ClipCascade_UI"
    private val reqPostNotifications = 1002
    private val uiHandler = Handler(Looper.getMainLooper())

    private var nsdManager: NsdManager? = null
    private var discoveryListener: NsdManager.DiscoveryListener? = null
    private val discoveredServers = LinkedHashSet<String>()

    private lateinit var historyList: ListView
    private lateinit var historyAdapter: ArrayAdapter<String>
    private var historyItems: List<ClipboardHistoryItem> = emptyList()
    private var historyRefreshTask: Runnable? = null

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        val prefs = getSharedPreferences("clipcascade", Context.MODE_PRIVATE)

        val layout = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(32, 64, 32, 32)
        }

        val titleText = TextView(this).apply {
            text = "ClipCascade Android 守护进程"
            textSize = 24f
            setPadding(0, 0, 0, 24)
        }

        val serverInput = EditText(this).apply {
            hint = "Server URL (http://ip:8080)"
            setText(prefs.getString("ServerURL", "") ?: "")
            layoutParams = LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1f)
        }
        val discoverMenuBtn = Button(this).apply {
            text = "▼"
            setOnClickListener {
                showDiscoveredServersMenu(this, serverInput)
            }
        }
        val serverRow = LinearLayout(this).apply {
            orientation = LinearLayout.HORIZONTAL
            addView(serverInput)
            addView(discoverMenuBtn)
        }

        val userInput = EditText(this).apply {
            hint = "Username"
            setText(prefs.getString("Username", "") ?: "")
        }
        val passInput = EditText(this).apply {
            hint = "Password"
            setText(prefs.getString("Password", "") ?: "")
        }
        val e2eeCheck = CheckBox(this).apply {
            text = "E2EE"
            isChecked = prefs.getBoolean("E2EE", true)
        }

        val btnSaveCfg = Button(this).apply {
            text = "保存连接配置"
            setOnClickListener {
                prefs.edit()
                    .putString("ServerURL", serverInput.text?.toString()?.trim() ?: "")
                    .putString("Username", userInput.text?.toString()?.trim() ?: "")
                    .putString("Password", passInput.text?.toString() ?: "")
                    .putBoolean("E2EE", e2eeCheck.isChecked)
                    .apply()
                Toast.makeText(this@MainActivity, "配置已保存", Toast.LENGTH_SHORT).show()
            }
        }

        val btnStartSvc = Button(this).apply {
            text = "启动/重启后台同步服务"
            setOnClickListener {
                prefs.edit()
                    .putString("ServerURL", serverInput.text?.toString()?.trim() ?: "")
                    .putString("Username", userInput.text?.toString()?.trim() ?: "")
                    .putString("Password", passInput.text?.toString() ?: "")
                    .putBoolean("E2EE", e2eeCheck.isChecked)
                    .apply()
                startCoreService()
            }
        }

        val btnReqOverlay = Button(this).apply {
            text = "1. 授予悬浮窗权限 (关键保活)"
            setOnClickListener { requestOverlayPermission() }
        }

        val btnReqA11y = Button(this).apply {
            text = "2. 前往开启无障碍服务"
            setOnClickListener { startActivity(Intent(Settings.ACTION_ACCESSIBILITY_SETTINGS)) }
        }

        val btnReqBattery = Button(this).apply {
            text = "3. 忽略电池优化 (防杀)"
            setOnClickListener { requestIgnoreBatteryOptimizations() }
        }

        val historyTitle = TextView(this).apply {
            text = "历史剪贴板（最新20条，点击可回填）"
            textSize = 16f
            setPadding(0, 24, 0, 8)
        }

        val btnRefreshHistory = Button(this).apply {
            text = "刷新历史"
            setOnClickListener { refreshHistoryList() }
        }

        val btnClearHistory = Button(this).apply {
            text = "清空历史"
            setOnClickListener {
                ClipboardHistoryStore.clear(this@MainActivity)
                refreshHistoryList()
            }
        }

        val historyActionRow = LinearLayout(this).apply {
            orientation = LinearLayout.HORIZONTAL
            addView(btnRefreshHistory)
            addView(btnClearHistory)
        }

        historyAdapter = ArrayAdapter(this, android.R.layout.simple_list_item_1, mutableListOf())
        historyList = ListView(this).apply {
            adapter = historyAdapter
            layoutParams = LinearLayout.LayoutParams(
                ViewGroup.LayoutParams.MATCH_PARENT,
                520
            )
            setOnItemClickListener { _, _, position, _ ->
                if (position < 0 || position >= historyItems.size) {
                    return@setOnItemClickListener
                }
                val text = historyItems[position].text
                val clipboard = getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager
                clipboard.setPrimaryClip(ClipData.newPlainText("ClipCascade", text))
                Toast.makeText(this@MainActivity, "已回填到系统剪贴板", Toast.LENGTH_SHORT).show()
            }
        }

        layout.addView(titleText)
        layout.addView(serverRow)
        layout.addView(userInput)
        layout.addView(passInput)
        layout.addView(e2eeCheck)
        layout.addView(btnSaveCfg)
        layout.addView(btnReqOverlay)
        layout.addView(btnReqA11y)
        layout.addView(btnReqBattery)
        layout.addView(btnStartSvc)
        layout.addView(historyTitle)
        layout.addView(historyActionRow)
        layout.addView(historyList)

        setContentView(layout)

        addDiscoveredServer(prefs.getString("ServerURL", "") ?: "", serverInput, discoverMenuBtn)
        startServerDiscovery(serverInput, discoverMenuBtn)
        refreshHistoryList()

        ensureNotificationPermissionIfNeeded()
        if ((prefs.getString("ServerURL", "") ?: "").isNotBlank()
            && (prefs.getString("Username", "") ?: "").isNotBlank()
            && (prefs.getString("Password", "") ?: "").isNotBlank()
        ) {
            startCoreService()
        }
    }

    override fun onResume() {
        super.onResume()
        startHistoryRefreshLoop()
    }

    override fun onPause() {
        stopHistoryRefreshLoop()
        super.onPause()
    }

    override fun onDestroy() {
        stopServerDiscovery()
        stopHistoryRefreshLoop()
        super.onDestroy()
    }

    private fun startCoreService() {
        val intent = Intent(this, ClipCascadeBackgroundService::class.java)
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            startForegroundService(intent)
        } else {
            startService(intent)
        }
        Toast.makeText(this, "服务已启动，请检查通知栏", Toast.LENGTH_SHORT).show()
    }

    private fun requestOverlayPermission() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.M) {
            if (!Settings.canDrawOverlays(this)) {
                Log.i(tag, "申请悬浮窗权限...")
                val intent = Intent(Settings.ACTION_MANAGE_OVERLAY_PERMISSION).apply {
                    data = Uri.parse("package:$packageName")
                }
                startActivity(intent)
            } else {
                Toast.makeText(this, "已拥有悬浮窗权限", Toast.LENGTH_SHORT).show()
            }
        }
    }

    private fun requestIgnoreBatteryOptimizations() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.M) {
            val intent = Intent(Settings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS).apply {
                data = Uri.parse("package:$packageName")
            }
            try {
                startActivity(intent)
            } catch (e: Exception) {
                Log.e(tag, "无法打开电池优化设置", e)
            }
        }
    }

    private fun ensureNotificationPermissionIfNeeded() {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.TIRAMISU) {
            return
        }
        val granted = ContextCompat.checkSelfPermission(
            this,
            Manifest.permission.POST_NOTIFICATIONS
        ) == PackageManager.PERMISSION_GRANTED
        if (!granted) {
            ActivityCompat.requestPermissions(
                this,
                arrayOf(Manifest.permission.POST_NOTIFICATIONS),
                reqPostNotifications
            )
        }
    }

    private fun startServerDiscovery(serverInput: EditText, discoverMenuBtn: Button) {
        stopServerDiscovery()
        nsdManager = getSystemService(Context.NSD_SERVICE) as? NsdManager ?: return

        val listener = object : NsdManager.DiscoveryListener {
            override fun onDiscoveryStarted(serviceType: String) {
                Log.i(tag, "mDNS discovery started: $serviceType")
            }

            override fun onStartDiscoveryFailed(serviceType: String, errorCode: Int) {
                Log.w(tag, "mDNS discovery failed to start: $errorCode")
            }

            override fun onStopDiscoveryFailed(serviceType: String, errorCode: Int) {
                Log.w(tag, "mDNS discovery failed to stop: $errorCode")
            }

            override fun onDiscoveryStopped(serviceType: String) {
                Log.i(tag, "mDNS discovery stopped: $serviceType")
            }

            override fun onServiceLost(serviceInfo: NsdServiceInfo) {
                // Keep history of discovered addresses for quick reconnect.
                Log.d(tag, "mDNS service lost: ${serviceInfo.serviceName}")
            }

            override fun onServiceFound(serviceInfo: NsdServiceInfo) {
                val serviceType = serviceInfo.serviceType ?: return
                if (!serviceType.equals("_clipcascade._tcp.", ignoreCase = true)) {
                    return
                }
                nsdManager?.resolveService(serviceInfo, object : NsdManager.ResolveListener {
                    override fun onResolveFailed(info: NsdServiceInfo, errorCode: Int) {
                        Log.d(tag, "mDNS resolve failed: $errorCode")
                    }

                    override fun onServiceResolved(info: NsdServiceInfo) {
                        val host = info.host?.hostAddress ?: info.host?.hostName ?: return
                        val url = buildHttpUrl(host, info.port)
                        addDiscoveredServer(url, serverInput, discoverMenuBtn)
                    }
                })
            }
        }

        discoveryListener = listener
        try {
            nsdManager?.discoverServices("_clipcascade._tcp.", NsdManager.PROTOCOL_DNS_SD, listener)
        } catch (e: Exception) {
            Log.w(tag, "mDNS discover start failed", e)
        }
    }

    private fun stopServerDiscovery() {
        val manager = nsdManager ?: return
        val listener = discoveryListener ?: return
        try {
            manager.stopServiceDiscovery(listener)
        } catch (_: Exception) {
        } finally {
            discoveryListener = null
        }
    }

    private fun addDiscoveredServer(url: String, serverInput: EditText, discoverMenuBtn: Button) {
        val normalized = url.trim()
        if (normalized.isBlank()) {
            return
        }
        val added = synchronized(discoveredServers) {
            discoveredServers.add(normalized)
        }
        if (!added) {
            return
        }
        runOnUiThread {
            updateDiscoverButton(discoverMenuBtn)
            val current = serverInput.text?.toString()?.trim().orEmpty()
            if (current.isBlank() || current == "http://localhost:8080") {
                serverInput.setText(normalized)
            }
        }
    }

    private fun updateDiscoverButton(btn: Button) {
        val count = synchronized(discoveredServers) { discoveredServers.size }
        btn.text = if (count > 0) "▼($count)" else "▼"
        btn.isEnabled = count > 0
    }

    private fun showDiscoveredServersMenu(anchor: Button, serverInput: EditText) {
        val options = synchronized(discoveredServers) { discoveredServers.toList() }
        if (options.isEmpty()) {
            Toast.makeText(this, "暂未发现局域网服务", Toast.LENGTH_SHORT).show()
            return
        }
        val menu = PopupMenu(this, anchor)
        options.forEachIndexed { index, url ->
            menu.menu.add(0, index, index, url)
        }
        menu.setOnMenuItemClickListener { item ->
            val selected = options.getOrNull(item.itemId) ?: return@setOnMenuItemClickListener false
            serverInput.setText(selected)
            true
        }
        menu.show()
    }

    private fun buildHttpUrl(host: String, port: Int): String {
        val cleanHost = host.trim().trimEnd('.')
        return if (cleanHost.contains(":") && !cleanHost.startsWith("[")) {
            "http://[$cleanHost]:$port"
        } else {
            "http://$cleanHost:$port"
        }
    }

    private fun refreshHistoryList() {
        historyItems = ClipboardHistoryStore.list(this, limit = 20)
        val display = if (historyItems.isEmpty()) {
            listOf("暂无历史记录")
        } else {
            val fmt = SimpleDateFormat("HH:mm:ss", Locale.getDefault())
            historyItems.map {
                val arrow = if (it.direction == "received") "↓" else "↑"
                val time = fmt.format(Date(it.timestamp))
                "$arrow $time  ${it.text}"
            }
        }
        historyAdapter.clear()
        historyAdapter.addAll(display)
        historyAdapter.notifyDataSetChanged()
    }

    private fun startHistoryRefreshLoop() {
        stopHistoryRefreshLoop()
        historyRefreshTask = object : Runnable {
            override fun run() {
                refreshHistoryList()
                uiHandler.postDelayed(this, 1500L)
            }
        }
        uiHandler.post(historyRefreshTask!!)
    }

    private fun stopHistoryRefreshLoop() {
        historyRefreshTask?.let { uiHandler.removeCallbacks(it) }
        historyRefreshTask = null
    }
}

