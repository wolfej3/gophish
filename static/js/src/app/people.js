// statusLabel colors a result's furthest status.
var statusLabel = function (status) {
    var map = {
        "Submitted Data": "danger",
        "Clicked Link": "warning",
        "Email Opened": "info",
        "Email Sent": "default",
        "Email Reported": "success"
    }
    var cls = map[status] || "default"
    return "<span class='label label-" + cls + "'>" + escapeHtml(status) + "</span>"
}

var showPerson = function (email) {
    api.people.detail(email)
        .success(function (p) {
            $("#personName").text((p.first_name || "") + " " + (p.last_name || ""))
            $("#personEmail").text(p.email)
            $("#personSummary").html(
                "<div class='col-sm-3'><strong>" + p.campaigns + "</strong> campaigns</div>" +
                "<div class='col-sm-2'><strong>" + p.opened + "</strong> opened</div>" +
                "<div class='col-sm-2'><strong>" + p.clicked + "</strong> clicked</div>" +
                "<div class='col-sm-2'><strong>" + p.submitted + "</strong> submitted</div>" +
                "<div class='col-sm-2'><strong>" + p.reported + "</strong> reported</div>")
            var hist = $("#personHistory tbody").empty()
            $.each(p.history, function (i, c) {
                hist.append($("<tr>")
                    .append($("<td>").text(c.name))
                    .append($("<td>").html(statusLabel(c.status)))
                    .append($("<td>").text(c.reported ? "Yes" : ""))
                    .append($("<td>").text(moment(c.send_date).format('MMM Do YYYY, h:mm a'))))
            })
            var tl = $("#personTimeline tbody").empty()
            $.each(p.timeline, function (i, e) {
                tl.append($("<tr>")
                    .append($("<td>").text(moment(e.time).format('MMM Do YYYY, h:mm:ss a')))
                    .append($("<td>").text(e.campaign_name))
                    .append($("<td>").text(e.message)))
            })
            $("#personModal").modal("show")
        })
        .error(function (data) { errorFlash(data.responseJSON.message) })
}

var load = function () {
    $("#peopleTable").hide()
    $("#emptyMessage").hide()
    $("#loading").show()
    api.people.get()
        .success(function (people) {
            $("#loading").hide()
            if (!people || people.length === 0) {
                $("#emptyMessage").show()
                return
            }
            $("#peopleTable").show()
            var table = $("#peopleTable").DataTable({ destroy: true, order: [[7, "desc"]] })
            table.clear()
            var rows = []
            $.each(people, function (i, p) {
                var name = ((p.first_name || "") + " " + (p.last_name || "")).trim()
                var last = p.last_event ? moment(p.last_event).format('MMM Do YYYY, h:mm a') : ""
                rows.push([
                    "<a href='#' class='person-link' data-email='" + encodeURIComponent(p.email) + "'>" + escapeHtml(name || "—") + "</a>",
                    escapeHtml(p.email),
                    p.campaigns, p.opened, p.clicked, p.submitted, p.reported, last
                ])
            })
            table.rows.add(rows).draw()
        })
        .error(function () {
            $("#loading").hide()
            errorFlash("Error fetching people")
        })
}

$(document).ready(function () {
    load()
    $("#peopleTable").on("click", ".person-link", function (e) {
        e.preventDefault()
        showPerson(decodeURIComponent($(this).attr("data-email")))
    })
})
