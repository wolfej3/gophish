// pageSize is how many audit log entries are fetched at a time.
const pageSize = 100

const actionNames = {
    login: "Login",
    login_failed: "Failed login",
    logout: "Logout",
    password_changed: "Password changed",
    "2fa_enabled": "Two-factor turned on",
    "2fa_disabled": "Two-factor turned off",
    recovery_codes_regenerated: "Recovery codes regenerated",
    sso_unlinked: "Single sign-on unlinked",
    impersonate: "Impersonation",
    api_request: "Change",
    api_request_failed: "Failed change",
    api_request_denied: "Denied change",
}

let oldestId = 0

// load fetches a page of audit log entries. When append is true, they're
// added below the entries already shown; otherwise they replace them.
const load = (append) => {
    let params = {
        limit: pageSize,
        username: $("#audit_username").val(),
        action: $("#audit_action").val(),
    }
    if (append) {
        params.before_id = oldestId
    } else {
        $("#auditTable tbody").empty()
        $("#auditTable").hide()
        $("#loading").show()
    }
    api.auditLog.get(params)
        .success((entries) => {
            $("#loading").hide()
            $("#auditTable").show()
            $.each(entries, (i, entry) => {
                oldestId = entry.id
                let row = $("<tr>")
                row.append($("<td>").text(moment(entry.created_at).format('MMMM Do YYYY, h:mm:ss a')))
                row.append($("<td>").text(entry.username))
                row.append($("<td>").text(actionNames[entry.action] || entry.action))
                row.append($("<td>").text(entry.target))
                row.append($("<td>").text(entry.details))
                row.append($("<td>").text(entry.ip_address))
                $("#auditTable tbody").append(row)
            })
            $("#auditOlder").toggleClass("hidden", entries.length < pageSize)
        })
        .error(() => {
            $("#loading").hide()
            errorFlash("Error fetching the audit log")
        })
}

$(document).ready(function () {
    load(false)
    $("#auditFilterForm").submit(function () {
        load(false)
        return false
    })
    $("#auditOlder").click(function () {
        load(true)
    })
})
