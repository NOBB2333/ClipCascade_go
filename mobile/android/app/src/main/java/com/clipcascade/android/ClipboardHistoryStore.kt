package com.clipcascade.android

import android.content.Context
import org.json.JSONArray
import org.json.JSONObject

data class ClipboardHistoryItem(
    val text: String,
    val direction: String,
    val timestamp: Long
)

object ClipboardHistoryStore {
    private const val PREFS_NAME = "clipcascade"
    private const val KEY_HISTORY = "ClipboardHistoryJson"
    private const val MAX_ITEMS = 50

    fun append(context: Context, text: String, direction: String) {
        val normalized = text.trim().replace("\n", " ")
        if (normalized.isBlank()) {
            return
        }

        val prefs = context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
        val arr = readJsonArray(prefs.getString(KEY_HISTORY, "[]"))

        // Avoid duplicate consecutive records.
        if (arr.length() > 0) {
            val last = arr.optJSONObject(0)
            if (last != null &&
                last.optString("text") == normalized &&
                last.optString("direction") == direction
            ) {
                return
            }
        }

        val item = JSONObject()
            .put("text", normalized.take(500))
            .put("direction", direction)
            .put("timestamp", System.currentTimeMillis())

        val next = JSONArray().put(item)
        for (i in 0 until arr.length()) {
            if (next.length() >= MAX_ITEMS) {
                break
            }
            next.put(arr.optJSONObject(i))
        }
        prefs.edit().putString(KEY_HISTORY, next.toString()).apply()
    }

    fun list(context: Context, limit: Int = 20): List<ClipboardHistoryItem> {
        val prefs = context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
        val arr = readJsonArray(prefs.getString(KEY_HISTORY, "[]"))
        val out = mutableListOf<ClipboardHistoryItem>()
        for (i in 0 until arr.length()) {
            if (out.size >= limit) {
                break
            }
            val item = arr.optJSONObject(i) ?: continue
            val text = item.optString("text")
            if (text.isBlank()) {
                continue
            }
            out.add(
                ClipboardHistoryItem(
                    text = text,
                    direction = item.optString("direction", "sent"),
                    timestamp = item.optLong("timestamp", 0L)
                )
            )
        }
        return out
    }

    fun clear(context: Context) {
        val prefs = context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
        prefs.edit().putString(KEY_HISTORY, "[]").apply()
    }

    private fun readJsonArray(raw: String?): JSONArray {
        return try {
            JSONArray(raw ?: "[]")
        } catch (_: Exception) {
            JSONArray()
        }
    }
}

