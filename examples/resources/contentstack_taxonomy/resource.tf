resource "contentstack_taxonomy" "topics" {
  uid         = "topics"
  name        = "Topics"
  description = "Editorial topics"

  lifecycle {
    prevent_destroy = true
  }
}
