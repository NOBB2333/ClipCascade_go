package com.clipcascade.android

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import android.content.Intent
import android.graphics.PixelFormat
import android.os.Binder
import android.os.Build
import android.os.IBinder
import android.util.Log
import android.view.WindowManager
import android.widget.LinearLayout
import androidx.core.content.ContextCompat
import bridge.bridge.Bridge
import bridge.bridge.Engine
import bridge.bridge.MessageCallback

class ClipCascadeBackgroundService : Service(), MessageCallback {

    private val TAG = "ClipCascade_BgSvc"
    
    private val notificationId = 1001
    private val channelId = "clipcascade_sync_channel"
    
    // 后台同步引擎实例
    @Volatile
    private var engine: Engine? = null
    @Volatile
    private var engineStarting = false
    
    private lateinit var clipboardManager: ClipboardManager
    private lateinit var notificationManager: NotificationManager
    private lateinit var windowManager: WindowManager
    
    // 用于透明悬浮窗的视图
    private var overlayLayout: LinearLayout? = null
    
    // 记录最后一次自己写入的记录，防止回环死循环同步
    private var lastWrittenText: String? = null

    // 给无障碍服务使用的 Binder
    private val binder = LocalBinder()

    inner class LocalBinder : Binder() {
        fun getService(): ClipCascadeBackgroundService = this@ClipCascadeBackgroundService
    }

    override fun onCreate() {
        super.onCreate()
        Log.i(TAG, "🟢 后台服务正在初始化...")
        clipboardManager = getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager
        notificationManager = getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
        windowManager = getSystemService(Context.WINDOW_SERVICE) as WindowManager
        
        startForegroundService()
        
        // 也可以在这里监听系统的剪贴板回调（虽然 Android 10+ 后台此回调无效，但在前台或悬浮窗期间生效）
        clipboardManager.addPrimaryClipChangedListener(onClipChangeListener)
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        Log.i(TAG, "🚀 后台服务已启动 (onStartCommand)")
        startEngineIfNeeded()
        
        // 保证服务被杀后系统自动重启它
        return START_STICKY
    }

    override fun onBind(intent: Intent?): IBinder {
        return binder
    }

    /**
     * 【核心黑科技】主动通过悬浮窗焦点获取剪贴板数据。
     * 就算我们在后台，只要弹出一个瞬间的悬浮窗，系统就会认为我们在前台。
     */
    fun requestClipboardReadForSync() {
        Log.i(TAG, "🔍 开始请求读取剪贴板...")
        startEngineIfNeeded()
        try {
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
                // 添加悬浮窗，抢夺前台焦点
                showInvisibleOverlay()
                
                // 此时系统判定我们具有交互焦点，读取剪贴板
                readClipboardAndSyncToServer()
                
                // 读完立刻销毁，无痕退出
                removeInvisibleOverlay()
            } else {
                // 老系统直接读
                readClipboardAndSyncToServer()
            }
        } catch (e: Exception) {
            Log.e(TAG, "悬浮窗或剪贴板操作失败: ${e.message}", e)
            removeInvisibleOverlay() // 确保即使崩溃也要尝试移掉视图
        }
    }

    /**
     * 将其他设备推过来的文本写入 Android 的剪贴板
     */
    override fun onMessage(payload: String, payloadType: String) {
        Log.i(TAG, "📥 收到远端推送: [类型=$payloadType] 内容=$payload")
        if (payloadType == "text") {
            try {
                // 如果恰好和我们上次发出去的一样，证明是自己环路过来的，丢弃
                if (payload == lastWrittenText) {
                    Log.d(TAG, "丢弃自回环文本。")
                    return
                }
                
                if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
                    showInvisibleOverlay()
                }
                
                // 写入前记录一下，防止 OnPrimaryClipChangedListener 再次触发发送
                lastWrittenText = payload
                val clip = ClipData.newPlainText("ClipCascade", payload)
                clipboardManager.setPrimaryClip(clip)
                ClipboardHistoryStore.append(this, payload, "received")
                Log.i(TAG, "✅ 成功写入 Android 系统剪贴板")
                
                if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
                    removeInvisibleOverlay()
                }
            } catch (e: Exception) {
                Log.e(TAG, "写入剪贴板失败: ${e.message}")
            }
        }
    }

    override fun onStatusChange(status: String) {
        Log.i(TAG, "网络状态变更: $status")
        when (status) {
            "connected" -> updateNotificationContent("已连接到服务器，实时同步中")
            "disconnected" -> updateNotificationContent("连接已断开，等待自动重连")
            "reconnecting" -> updateNotificationContent("正在重连服务器...")
            "error" -> updateNotificationContent("连接错误")
            else -> updateNotificationContent("状态: $status")
        }
    }

    private val onClipChangeListener = ClipboardManager.OnPrimaryClipChangedListener {
        // 这个回调只有在我们处于（真前台、假前台/悬浮窗期间）才会被系统触发
        // 但无障碍服务其实已经帮我们在全天候盯着剪贴板了，所以这里可以做辅助，或者干脆只信无障碍
        Log.d(TAG, "系统 OnPrimaryClipChanged 触发了！")
    }

    private fun readClipboardAndSyncToServer() {
        val clip = clipboardManager.primaryClip ?: return
        if (clip.itemCount > 0) {
            val textToSync = clip.getItemAt(0).text?.toString()
            if (!textToSync.isNullOrEmpty()) {
                if (textToSync == lastWrittenText) {
                    Log.d(TAG, "这是刚才自己收到的文本，防回环阻断上传。")
                    return
                }
                
                Log.i(TAG, "📤 准备将其发送给其他设备: $textToSync")
                lastWrittenText = textToSync
                try {
                    engine?.sendClipboard(textToSync, "text")
                    ClipboardHistoryStore.append(this, textToSync, "sent")
                } catch (e: Exception) {
                    Log.e(TAG, "发送剪贴板失败: ${e.message}", e)
                }
            }
        } else {
            Log.d(TAG, "剪贴板为空")
        }
    }

    private fun startEngineIfNeeded() {
        if (engine != null || engineStarting) {
            return
        }
        val prefs = getSharedPreferences("clipcascade", Context.MODE_PRIVATE)
        val url = prefs.getString("ServerURL", "") ?: ""
        val user = prefs.getString("Username", "") ?: ""
        val pass = prefs.getString("Password", "") ?: ""
        val e2e = prefs.getBoolean("E2EE", true)

        if (url.isBlank() || user.isBlank() || pass.isBlank()) {
            updateNotificationContent("未配置服务器账号，请先在应用内填写配置")
            return
        }

        engineStarting = true
        Thread {
            try {
                val created = Bridge.newEngine(url, user, pass, e2e)
                created.setCallback(this)
                created.start()
                engine = created
                updateNotificationContent("已连接到服务器，实时同步中")
            } catch (e: Exception) {
                engine = null
                Log.e(TAG, "Go 引擎启动失败", e)
                updateNotificationContent("连接失败: ${e.message}")
            } finally {
                engineStarting = false
            }
        }.start()
    }

    // ==================
    // 悬浮窗控制部分
    // ==================
    private fun showInvisibleOverlay() {
        if (overlayLayout != null) {
            return // 已经有了
        }

        overlayLayout = LinearLayout(this).apply {
            alpha = 0f // 完全透明
            val color = ContextCompat.getColor(this@ClipCascadeBackgroundService, android.R.color.transparent)
            setBackgroundColor(color)
            orientation = LinearLayout.VERTICAL
        }
        
        val layoutParams = WindowManager.LayoutParams(
            1, 1, // 越小越好，1x1 像素
            WindowManager.LayoutParams.TYPE_APPLICATION_OVERLAY, // API 26+ Android 8.0 强制
            // 设置不可触摸，也不截获点击，防止影响用户原本的操作
            WindowManager.LayoutParams.FLAG_NOT_TOUCHABLE or WindowManager.LayoutParams.FLAG_NOT_TOUCH_MODAL or WindowManager.LayoutParams.FLAG_NOT_FOCUSABLE,
            PixelFormat.TRANSPARENT
        )
        // 去除焦点标记，改为只接收一次轻微获取焦点。这里是一个微妙的点，如果系统不给焦点，可以用 FLAG_NOT_TOUCH_MODAL 试试。
        // 但根据 CopyCat 的经验，只要 mView 被添加到 WindowManager，就已经算作是“该应用持有的活跃视图”了。
        layoutParams.flags = WindowManager.LayoutParams.FLAG_NOT_TOUCHABLE or WindowManager.LayoutParams.FLAG_NOT_TOUCH_MODAL
        
        windowManager.addView(overlayLayout, layoutParams)
        Log.v(TAG, "👻 [Hack] 添加了 1x1 透明悬浮窗骗取焦点")
    }

    private fun removeInvisibleOverlay() {
        overlayLayout?.let {
            try {
                windowManager.removeView(it)
                Log.v(TAG, "👻 [Hack] 移除了悬浮窗")
            } catch (e: Exception) {
                Log.w(TAG, "移除悬浮窗出错: ${e.message}")
            }
        }
        overlayLayout = null
    }

    // ==================
    // 前台服务及通知部分
    // ==================
    private fun startForegroundService() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            val channel = NotificationChannel(
                channelId,
                getString(R.string.notification_channel_name),
                NotificationManager.IMPORTANCE_LOW // 低优先级，避免打扰，不发声
            )
            notificationManager.createNotificationChannel(channel)
        }

        // 点击通知栏打开主 Activity (可选，这里先留空或指向 MainActivity)
        val pendingIntent = PendingIntent.getActivity(
            this, 0, 
            Intent(this, MainActivity::class.java).apply {
                flags = Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TASK
            },
            PendingIntent.FLAG_IMMUTABLE
        )

        val notification = Notification.Builder(this, channelId)
            .setContentTitle(getString(R.string.notification_title))
            .setContentText("服务正在后台时刻准备着...")
            // 前台通知必须有小图标，否则在部分系统上会直接启动失败。
            .setSmallIcon(android.R.drawable.stat_notify_sync)
            .setContentIntent(pendingIntent)
            .setOngoing(true) // 划不掉
            .build()
            
        // 适配 Android 14 要求的类型
        try {
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
                startForeground(notificationId, notification, android.content.pm.ServiceInfo.FOREGROUND_SERVICE_TYPE_DATA_SYNC)
            } else {
                startForeground(notificationId, notification)
            }
        } catch (e: Exception) {
            Log.e(TAG, "启动前台服务失败，降级不带 Type 启动", e)
            startForeground(notificationId, notification)
        }
    }

    private fun updateNotificationContent(text: String) {
        val notification = Notification.Builder(this, channelId)
            .setContentTitle(getString(R.string.notification_title))
            .setContentText(text)
            .setSmallIcon(android.R.drawable.stat_notify_sync)
            .setOngoing(true)
            .build()
        notificationManager.notify(notificationId, notification)
    }

    override fun onDestroy() {
        Log.i(TAG, "🔴 后台服务被销毁")
        clipboardManager.removePrimaryClipChangedListener(onClipChangeListener)
        try {
            engine?.stop()
        } catch (_: Exception) {
        }
        engine = null
        removeInvisibleOverlay()
        
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.N) {
            stopForeground(STOP_FOREGROUND_REMOVE)
        } else {
            @Suppress("DEPRECATION")
            stopForeground(true)
        }
        super.onDestroy()
    }
}
