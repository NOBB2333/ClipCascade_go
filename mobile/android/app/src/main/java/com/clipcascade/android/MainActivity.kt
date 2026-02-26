package com.clipcascade.android

import android.app.Activity
import android.Manifest
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.provider.Settings
import android.util.Log
import android.widget.Button
import android.widget.LinearLayout
import android.widget.TextView
import android.widget.Toast
import androidx.core.app.ActivityCompat
import androidx.core.content.ContextCompat

class MainActivity : Activity() {

    private val TAG = "ClipCascade_UI"
    private val REQ_POST_NOTIFICATIONS = 1002

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        
        // 极简的原生测试界面 (实际可以换成 Fyne 或更美的 Compose)
        val layout = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(32, 64, 32, 32)
        }

        val titleText = TextView(this).apply {
            text = "ClipCascade Android 守护进程"
            textSize = 24f
            setPadding(0, 0, 0, 32)
        }

        val btnStartSvc = Button(this).apply {
            text = "启动核心后台服务"
            setOnClickListener {
                startCoreService()
            }
        }

        val btnReqOverlay = Button(this).apply {
            text = "1. 授予悬浮窗权限 (关键保活)"
            setOnClickListener {
                requestOverlayPermission()
            }
        }

        val btnReqA11y = Button(this).apply {
            text = "2. 前往开启无障碍服务"
            setOnClickListener {
                startActivity(Intent(Settings.ACTION_ACCESSIBILITY_SETTINGS))
            }
        }

        val btnReqBattery = Button(this).apply {
            text = "3. 忽略电池优化 (防杀)"
            setOnClickListener {
                requestIgnoreBatteryOptimizations()
            }
        }

        layout.addView(titleText)
        layout.addView(btnReqOverlay)
        layout.addView(btnReqA11y)
        layout.addView(btnReqBattery)
        layout.addView(btnStartSvc)

        setContentView(layout)
        ensureNotificationPermissionIfNeeded()
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
                Log.i(TAG, "申请悬浮窗权限...")
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
                Log.e(TAG, "无法打开电池优化设置", e)
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
                REQ_POST_NOTIFICATIONS
            )
        }
    }
}
